package crio

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/distribution/reference"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"go.podman.io/storage"
	"go.podman.io/storage/types"
	"k8s.io/klog/v2"
	k8smount "k8s.io/utils/mount"
)

type snapshotMounter struct {
	imageStore storage.Store
}

func NewMounter(socketPath string) *backend.SnapshotMounter {
	store, err := storage.GetStore(fetchCriOConfigOrDie(socketPath))
	if err != nil {
		klog.Fatalf("unable to create image store: %s", err)
	}

	return backend.NewMounter(&snapshotMounter{
		imageStore: store,
	})
}

func (s snapshotMounter) Mount(_ context.Context, key backend.SnapshotKey, target backend.MountTarget, ro bool) error {
	src, err := s.imageStore.Mount(string(key), "")
	if err != nil {
		klog.Errorf("unable to mount snapshot %q: %s", key, err)
		return err
	}

	mountOpts := []string{"rbind"}
	if ro {
		mountOpts = append(mountOpts, "ro")
	}

	if err = k8smount.New("").Mount(src, string(target), "", mountOpts); err != nil {
		klog.Errorf("unable to bind %q to %q: %s", src, target, err)
		return err
	}

	return nil
}

func (s snapshotMounter) Unmount(_ context.Context, target backend.MountTarget) error {
	if err := k8smount.New("").Unmount(string(target)); err != nil {
		klog.Errorf("unable to unmount %q: %s", target, err)
		return err
	}

	return nil
}

func (s snapshotMounter) ImageExists(ctx context.Context, image reference.Named) bool {
	if _, err := s.imageStore.Image(image.String()); err != nil {
		klog.Errorf("unable to retrieve the local image %q: %s", image, err)
		return false
	}

	return true
}

func (s snapshotMounter) GetImageIDOrDie(ctx context.Context, image reference.Named) string {
	img, err := s.imageStore.Image(image.String())
	if err != nil {
		klog.Fatalf("unable to retrieve local image %q: %s", image, err)
	}

	return img.ID
}

func (s snapshotMounter) PrepareReadOnlySnapshot(
	_ context.Context, imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	return s.prepareSnapshot(imageID, key, metadata, &storage.ContainerOptions{MountOpts: []string{"ro"}})
}

func (s snapshotMounter) PrepareRWSnapshot(
	_ context.Context, imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	return s.prepareSnapshot(imageID, key, metadata, nil)
}

func (s snapshotMounter) prepareSnapshot(
	imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata, opts *storage.ContainerOptions,
) error {
	var metaString string
	if metadata != nil {
		metaString = metadata.Encode()
	}

	if opts != nil {
		klog.Infof("create ro snapshot %q for image %q with metadata %#v(compressed length %d)",
			key, imageID, metadata, len(metaString))
	} else {
		klog.Infof("create rw snapshot %q for image %q with metadata %#v(compressed length %d)",
			key, imageID, metadata, len(metaString))
	}

	c, err := s.imageStore.Container(string(key))
	if err == nil {
		if c.ImageID != imageID {
			return fmt.Errorf("found existed snapshot %q with different image %#v", key, c.ImageID)
		}

		if metadata == nil {
			klog.Infof("found existed snapshot %q, use it", key)
			return nil
		}

		if c.Metadata == "" {
			return fmt.Errorf("found existed snapshot %q without metadata", key)
		}

		existedMetadata := make(backend.SnapshotMetadata)
		if err := existedMetadata.Decode(c.Metadata); err == nil {
			return fmt.Errorf("found existed snapshot %q with unknown metadata %s", key, c.Metadata)
		}

		for k, v := range metadata {
			if !reflect.DeepEqual(v, existedMetadata[k]) {
				return fmt.Errorf("found existed snapshot %q with different configuration %#v", key,
					existedMetadata)
			}
		}

		klog.Infof("found existed snapshot %q, use it", key)
		return nil
	}

	if _, err = s.imageStore.CreateContainer(string(key), nil, imageID, "", metaString, opts); err != nil {
		klog.Errorf("unable to create container for image %q: %s", imageID, err)
		return err
	}

	return nil
}

func (s snapshotMounter) UpdateSnapshotMetadata(
	_ context.Context, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	metaString := metadata.Encode()
	klog.Infof("update metadata of snapshot %q to %#v(compressed length %d)", key, metadata, len(metaString))
	err := s.imageStore.SetMetadata(string(key), metaString)
	if err != nil {
		klog.Errorf("unable to update metadata of snapshot %q: %s", key, err)
		return err
	}

	return err
}

func (s snapshotMounter) DestroySnapshot(_ context.Context, key backend.SnapshotKey) error {
	klog.Infof("unmount container %q", key)
	if stillMounted, err := s.imageStore.Unmount(string(key), true); err != nil || stillMounted {
		klog.Errorf("unable to unmount %q: %t %s", key, stillMounted, err)
	}

	klog.Infof("remove container %q", key)
	if err := s.imageStore.DeleteContainer(string(key)); err != nil {
		klog.Errorf("unable to destroy container of image %q: %s", key, err)
		return err
	}

	return nil
}

func (s snapshotMounter) ListSnapshots(context.Context) (ss []backend.SnapshotMetadata, err error) {
	containers, err := s.imageStore.Containers()
	if err != nil {
		klog.Errorf("unable to list snapshots: %s", err)
		return nil, err
	}

	klog.Infof("found %d containers", len(containers))

	for _, c := range containers {
		if c.Metadata != "" {
			metadata := make(backend.SnapshotMetadata)
			if err := metadata.Decode(c.Metadata); err == nil {
				metadata.SetSnapshotKey(c.ID)
				ss = append(ss, metadata)
				klog.Infof("got ro snapshot %q with targets %#v", c.ID, metadata.GetTargets())
			} else {
				klog.Warningf("unable to decode the metadata of snapshot %q: %s. it may be not a snapshot",
					c.ID, err)
			}
		}
	}

	return
}

type crioRootConfig struct {
	Crio struct {
		Root           string   `toml:"root"`
		RunRoot        string   `toml:"runroot"`
		Storage        string   `toml:"storage_driver"`
		StorageOptions []string `toml:"storage_option"`
	} `toml:"crio"`
}

func fetchCriOConfigOrDie(socketPath string) types.StoreOptions {
	cli := &http.Client{Transport: &http.Transport{
		DisableCompression: true,
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 32*time.Second)
		},
	}}

	req, err := http.NewRequest("GET", "/config", nil)
	if err != nil {
		klog.Fatalf("unable to create http request: %s", err)
	}

	req.Host = "crio"
	req.URL.Host = socketPath
	req.URL.Scheme = "http"

	resp, err := cli.Do(req)
	if err != nil {
		klog.Fatalf("unable to fetch cri-o configuration: %s", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Fatalf("unable to read cri-o configuration response: %s", err)
	}

	c := &crioRootConfig{}
	if _, err = toml.Decode(string(body), c); err != nil {
		klog.Fatalf("unable to decode cri-o configuration: %s", err)
	}

	klog.Infof("cri-o configuration: %#v", c)

	return types.StoreOptions{
		RunRoot:            c.Crio.RunRoot,
		GraphRoot:          c.Crio.Root,
		GraphDriverName:    c.Crio.Storage,
		GraphDriverOptions: c.Crio.StorageOptions,
	}
}
