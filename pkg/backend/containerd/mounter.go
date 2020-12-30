package containerd

import (
	"context"
	"fmt"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/snapshots"
	"github.com/golang/glog"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"strings"
	"sync"
	"time"
)

type mounter struct {
	containerdEndpoint string
	namespace          string
	snapshotLock       sync.Mutex
	snapshotterName    string
}

func NewMounter(endpoint, namespace string) backend.Mounter {
	return &mounter{
		containerdEndpoint: endpoint,
		namespace:          namespace,
		snapshotterName:    containerd.DefaultSnapshotter,
	}
}

func genSnapshotKey(parent string) string {
	return fmt.Sprintf("csi-image.warm-metal.tech-%s", parent)
}

const (
	targetLabelPrefix   = "csi-image.warm-metal.tech/target"
	imageLabelPrefix          = "csi-image.warm-metal.tech/image"
	volumeIdLabelPrefix = "csi-image.warm-metal.tech/id"
)

func defaultSnapshotLabels() map[string]string {
	return map[string]string{
		"containerd.io/gc.root": time.Now().UTC().Format(time.RFC3339),
	}
}

func genTargetLabel(target string) string {
	return fmt.Sprintf("%s|%s", targetLabelPrefix, target)
}

func withTarget(labels map[string]string, target string) map[string]string {
	labels[genTargetLabel(target)] = "√"
	return labels
}

func genVolumeIdLabel(volumeId string) string {
	return fmt.Sprintf("%s|%s", volumeIdLabelPrefix, volumeId)
}

func withVolumeId(labels map[string]string, volumeId string) map[string]string {
	labels[genVolumeIdLabel(volumeId)] = "√"
	return labels
}

func withImage(labels map[string]string, image string) map[string]string {
	labels[fmt.Sprintf("%s|%s", imageLabelPrefix, image)] = "√"
	return labels
}

func (m *mounter) getImageRootFSChainID(
	ctx context.Context, c *containerd.Client, image string, pull bool,
) (parent string, err error) {
	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		glog.Errorf("fail to normalize image: %s, %s", image, err)
		return
	}

	localImage, err := c.GetImage(ctx, namedRef.String())
	if err != nil {
		if errors.Cause(err) != errdefs.ErrNotFound && !pull {
			glog.Errorf("fail to retrieve local image: %s, %s", namedRef, err)
			return
		}

		glog.Infof("no local image found. Pull %s", namedRef)
		localImage, err = c.Pull(ctx, namedRef.String(), containerd.WithPullUnpack, containerd.WithSchema1Conversion)
		if err != nil {
			glog.Errorf("fail to pull image: %s, %s", namedRef, err)
			return
		}
	} else {
		glog.Infof("found local image %s", namedRef)
	}

	if err = localImage.Unpack(ctx, m.snapshotterName); err != nil {
		glog.Errorf("fail to unpack image: %s, %s, %s", namedRef, m.snapshotterName, err)
		return
	}

	glog.Infof("image %s unpacked", namedRef)
	diffIDs, err := localImage.RootFS(ctx)
	if err != nil {
		return
	}

	parent = identity.ChainID(diffIDs).String()
	return
}

func (m *mounter) refSnapshot(
	ctx context.Context, c *containerd.Client, volumeId, image, target string,
) (mounts []mount.Mount, err error) {
	parent, err := m.getImageRootFSChainID(ctx, c, image, true)
	if err != nil {
		glog.Errorf("fail to get rootfs of image %s: %s", image, err)
		return
	}

	glog.Infof("prepare %s", parent)

	snapshotter := c.SnapshotService(m.snapshotterName)
	key := genSnapshotKey(parent)

	m.snapshotLock.Lock()
	defer m.snapshotLock.Unlock()

	mounts, err = snapshotter.View(ctx, key, parent, snapshots.WithLabels(
		withImage(withVolumeId(withTarget(defaultSnapshotLabels(), target), volumeId), image),
	))
	if err == nil {
		return
	}

	if !errdefs.IsAlreadyExists(err) {
		return
	}

	info, err := snapshotter.Stat(ctx, key)
	if err != nil {
		return
	}

	withImage(withVolumeId(withTarget(info.Labels, target), volumeId), image)
	_, err = snapshotter.Update(ctx, info)
	if err != nil {
		return
	}

	mounts, err = snapshotter.Mounts(ctx, key)
	return
}

func (m *mounter) unrefSnapshot(
	ctx context.Context, c *containerd.Client, volumeId, target string,
) error {
	snapshotter := c.SnapshotService(m.snapshotterName)
	targetLabel := genTargetLabel(target)

	unref := func(info snapshots.Info, targetLabel string) error {
		glog.Infof("unref snapshot %s, parent %s", info.Name, info.Parent)
		delete(info.Labels, targetLabel)
		referred := false
		for label := range info.Labels {
			if strings.HasPrefix(label, targetLabelPrefix) {
				glog.Infof("snapshot %s is also mounted to %s", info.Name, label)
				referred = true
				break
			}
		}

		if !referred {
			glog.Infof("no other mount refs snapshot %s, remove it", info.Name)
			return snapshotter.Remove(ctx, info.Name)
		} else {
			_, err := snapshotter.Update(ctx, info)
			return err
		}
	}

	m.snapshotLock.Lock()
	defer m.snapshotLock.Unlock()

	matchCounter := 0
	return snapshotter.Walk(ctx, func(ctx context.Context, info snapshots.Info) error {
		if len(info.Labels) == 0 {
			return nil
		}

		if _, found := info.Labels[targetLabel]; !found {
			return nil
		}

		volumeIdLabel := genVolumeIdLabel(volumeId)
		if _, found := info.Labels[volumeIdLabel]; !found {
			panic(fmt.Sprintf("snapshot doesn't belong to volume %s: %#v", volumeId, info.Labels))
		}

		if matchCounter > 0 {
			panic("at most 1 snapshot can match the condition")
		}

		matchCounter++

		glog.Infof(`found snapshot %s for volume %s. prepare to unref it.`, info.Name, volumeId)
		return unref(info, targetLabel)
	},
		`kind==view`, fmt.Sprintf(`labels."%s"==%s`, targetLabel, "√"))
}

func (m *mounter) Mount(ctx context.Context, volumeId, image, target string) (err error) {
	c, err := containerd.New(m.containerdEndpoint, containerd.WithDefaultNamespace(m.namespace))
	if err != nil {
		glog.Errorf("fail to create containerd client: %s", err)
		return
	}

	mounts, err := m.refSnapshot(ctx, c, volumeId, image, target)
	if err != nil {
		glog.Errorf("fail to prepare image %s: %s", image, err)
		return
	}

	defer func() {
		if err != nil {
			glog.Errorf("found error %s. Prepare removing the snapshot just created", err)
			if err := m.unrefSnapshot(ctx, c, volumeId, target); err != nil {
				glog.Errorf("fail to recycle snapshot: %s, %s", image, err)
			}
		}
	}()

	err = mount.All(mounts, target)
	if err != nil {
		glog.Errorf("fail to mount image %s to %s: %s", image, target, err)
	} else {
		glog.Infof("image %s mounted", image)
	}

	return err
}

func (m *mounter) Unmount(ctx context.Context, volumeId, target string) (err error) {
	c, err := containerd.New(m.containerdEndpoint, containerd.WithDefaultNamespace(m.namespace))
	if err != nil {
		glog.Errorf("fail to create containerd client: %s", err)
		return
	}

	if err = mount.UnmountAll(target, 0); err != nil {
		glog.Errorf("fail to unmount %s: %s", target, err)
		return err
	}

	glog.Infof("%s unmounted", target)

	if err = m.unrefSnapshot(ctx, c, volumeId, target); err != nil {
		glog.Errorf("fail to unref snapshot of volume %s: %s", volumeId, err)
		return
	}

	return
}
