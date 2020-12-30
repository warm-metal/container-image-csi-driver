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
	targetLabel = "csi-image.warm-metal.tech/target"
	imageLabel  = "csi-image.warm-metal.tech/image"
)

func genTargetLabelKey(target string) string {
	return fmt.Sprintf("%s/%s", targetLabel, target)
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
	ctx context.Context, c *containerd.Client, image, parent, target string,
) (mounts []mount.Mount, err error) {
	snapshotter := c.SnapshotService(m.snapshotterName)
	key := genSnapshotKey(parent)
	targetLabelKey := genTargetLabelKey(target)
	m.snapshotLock.Lock()
	defer m.snapshotLock.Unlock()
	mounts, err = snapshotter.View(ctx, key, parent, snapshots.WithLabels(map[string]string{
		"containerd.io/gc.root": time.Now().UTC().Format(time.RFC3339),
		imageLabel:              image,
		targetLabelKey:          "√", // snapshotter ignores labels w/o values
	}))

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

	info.Labels[targetLabelKey] = "√"
	_, err = snapshotter.Update(ctx, info)
	if err != nil {
		return
	}

	mounts, err = snapshotter.Mounts(ctx, key)
	return
}

func (m *mounter) unrefSnapshot(
	ctx context.Context, c *containerd.Client, image, parent, target string,
) error {
	snapshotter := c.SnapshotService(m.snapshotterName)
	targetLabelKey := genTargetLabelKey(target)

	unref := func(info snapshots.Info, targetLabelKey string) error {
		glog.Infof("unref snapshot %s, parent %s", info.Name, info.Parent)
		delete(info.Labels, targetLabelKey)
		referred := false
		for label := range info.Labels {
			if strings.HasPrefix(label, targetLabel) {
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

	if len(parent) > 0 {
		key := genSnapshotKey(parent)
		info, err := snapshotter.Stat(ctx, key)
		if err != nil {
			glog.Errorf(`snapshot "%s" not found: %s`, key, err)
		} else {
			if len(info.Labels) > 0 && info.Labels[imageLabel] == image {
				glog.Infof(`found snapshot "%s" for image "%s": %#v`, key, image, info.Labels)
				if _, found := info.Labels[targetLabelKey]; found {
					return unref(info, targetLabelKey)
				}
			}
		}
	}

	// fallback to walk all snapshots and find out the target snapshot
	glog.Infof("no snapshot matches %s/%s. fallback to walk all snapshots", image, parent)
	return snapshotter.Walk(ctx, func(ctx context.Context, info snapshots.Info) error{
		if len(info.Labels) == 0 {
			return nil
		}
		if _, found := info.Labels[targetLabelKey]; !found {
			return nil
		}

		glog.Infof(`found snapshot %s for %s. prepare to unref it.`, info.Name, image)
		return unref(info, targetLabelKey)
	},
	`kind==view`, fmt.Sprintf(`labels."%s"==%s`, imageLabel, image))
}

func (m *mounter) Mount(ctx context.Context, image, target string) (err error) {
	c, err := containerd.New(m.containerdEndpoint, containerd.WithDefaultNamespace(m.namespace))
	if err != nil {
		glog.Errorf("fail to create containerd client: %s", err)
		return
	}

	parent, err := m.getImageRootFSChainID(ctx, c, image, true)
	if err != nil {
		glog.Errorf("fail to get rootfs of image %s: %s", image, err)
		return
	}

	glog.Infof("prepare %s", parent)

	mounts, err := m.refSnapshot(ctx, c, image, parent, target)
	if err != nil {
		glog.Errorf("fail to prepare image: %s, %s, %s, %s", image, parent, m.snapshotterName, err)
		return
	}

	defer func() {
		if err != nil {
			glog.Errorf("found error %s. Prepare removing the snapshot just created", err)
			if err := m.unrefSnapshot(ctx, c, image, parent, target); err != nil {
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

func (m *mounter) Unmount(ctx context.Context, image, target string) (err error) {
	c, err := containerd.New(m.containerdEndpoint, containerd.WithDefaultNamespace(m.namespace))
	if err != nil {
		glog.Errorf("fail to create containerd client: %s", err)
		return
	}

	parent, err := m.getImageRootFSChainID(ctx, c, image, false)
	if err != nil {
		glog.Errorf("fail to get rootfs of image %s: %s", image, err)
	}

	if err = mount.UnmountAll(target, 0); err != nil {
		glog.Errorf("fail to unmount %s: %s", target, err)
		return err
	}

	glog.Infof("%s unmounted", target)

	err = m.unrefSnapshot(ctx, c, image, parent, target)
	if err != nil {
		glog.Errorf("fail to rm snapshot %s: %s", image, err)
		return
	}

	return
}
