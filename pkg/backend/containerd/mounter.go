package containerd

import (
	"context"
	"fmt"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/snapshots"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"golang.org/x/xerrors"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/util"
	"os"
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

func NewMounter(endpoint string) backend.Mounter {
	addr, _, err := util.GetAddressAndDialer(endpoint)
	if err != nil {
		klog.Fatalf("%s:%s", endpoint, err)
	}

	return &mounter{
		containerdEndpoint: addr,
		namespace:          "k8s.io",
		snapshotterName:    containerd.DefaultSnapshotter,
	}
}

func genSnapshotKey(parent string) string {
	return fmt.Sprintf("csi-image.warm-metal.tech-%s", parent)
}

const (
	targetLabelPrefix   = "csi-image.warm-metal.tech/target"
	imageLabelPrefix    = "csi-image.warm-metal.tech/image"
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
	ctx context.Context, c *containerd.Client, puller remoteimage.Puller, image string, pull, always bool,
) (parent string, err error) {
	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		klog.Errorf("fail to normalize image: %s, %s", image, err)
		return
	}

	if always {
		klog.Infof(`Pull image "%s"`, namedRef)
		if err = puller.Pull(ctx); err != nil {
			klog.Errorf("fail to pull image: %s, %s", namedRef, err)
			return
		}
	}

	localImage, err := c.GetImage(ctx, namedRef.String())
	if err != nil {
		if errors.Cause(err) != errdefs.ErrNotFound && !pull {
			klog.Errorf("fail to retrieve local image: %s, %s", namedRef, err)
			return
		}

		klog.Infof(`no local image found. Pull image "%s"`, namedRef)

		if err = puller.Pull(ctx); err != nil {
			klog.Errorf("fail to pull image: %s, %s", namedRef, err)
			return
		}

		localImage, err = c.GetImage(ctx, namedRef.String())
		if err != nil {
			panic(err)
		}
	}

	if err = localImage.Unpack(ctx, m.snapshotterName); err != nil {
		klog.Errorf("fail to unpack image: %s, %s, %s", namedRef, m.snapshotterName, err)
		return
	}

	klog.Infof("image %s unpacked", namedRef)
	diffIDs, err := localImage.RootFS(ctx)
	if err != nil {
		return
	}

	parent = identity.ChainID(diffIDs).String()
	return
}

func (m *mounter) refSnapshot(
	ctx context.Context, c *containerd.Client, puller remoteimage.Puller, volumeId, image, target string, opts *backend.MountOptions,
) (mounts []mount.Mount, err error) {
	parent, err := m.getImageRootFSChainID(ctx, c, puller, image, true, opts.PullAlways)
	if err != nil {
		klog.Errorf("fail to get rootfs of image %s: %s", image, err)
		return
	}

	klog.Infof("prepare %s", parent)

	snapshotter := c.SnapshotService(m.snapshotterName)
	keySuffix := opts.VolumeId
	if opts.ReadOnly {
		// ReadOnly volumes can share a single snapshot.
		keySuffix = parent
	}
	key := genSnapshotKey(keySuffix)

	m.snapshotLock.Lock()
	defer m.snapshotLock.Unlock()

	mounts, err = snapshotter.Prepare(ctx, key, parent, snapshots.WithLabels(
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
		klog.Infof("unref snapshot %s, parent %s", info.Name, info.Parent)
		delete(info.Labels, targetLabel)
		referred := false
		for label := range info.Labels {
			if strings.HasPrefix(label, targetLabelPrefix) {
				klog.Infof("snapshot %s is also mounted to %s", info.Name, label)
				referred = true
				break
			}
		}

		if !referred {
			klog.Infof("no other mount refs snapshot %s, remove it", info.Name)
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

		klog.Infof(`found snapshot %s for volume %s. prepare to unref it.`, info.Name, volumeId)
		return unref(info, targetLabel)
	},
		`kind==view`, fmt.Sprintf(`labels."%s"==%s`, targetLabel, "√"))
}

func (m *mounter) Mount(ctx context.Context, puller remoteimage.Puller, volumeId, image, target string, opts *backend.MountOptions) (err error) {
	// FIXME lease
	c, err := containerd.New(m.containerdEndpoint, containerd.WithDefaultNamespace(m.namespace))
	if err != nil {
		klog.Fatalf("containerd connection is broken because the mounted unix socket somehow dose not work,"+
			"recreate the container may fix: %s", err)
	}

	mounts, err := m.refSnapshot(ctx, c, puller, volumeId, image, target, opts)
	if err != nil {
		klog.Errorf("fail to prepare image %s: %s", image, err)
		return
	}

	defer func() {
		if err != nil {
			klog.Errorf("found error %s. Removing the snapshot just created", err)
			if err := m.unrefSnapshot(ctx, c, volumeId, target); err != nil {
				klog.Errorf("fail to recycle snapshot: %s, %s", image, err)
			}
		}
	}()

	err = mount.All(mounts, target)
	if err != nil {
		mountsErr := describeMounts(mounts, target)
		if len(mountsErr) > 0 {
			err = xerrors.New(mountsErr)
		}

		klog.Errorf("fail to mount image %s: %s", image, err)
	} else {
		klog.Infof("image %s mounted", image)
	}

	return err
}

func describeMounts(mounts []mount.Mount, target string) string {
	prefixes := []string{
		"lowerdir=",
		"upperdir=",
		"workdir=",
	}

	var err error
	for _, m := range mounts {
		if m.Type == "overlay" {
			for _, opt := range m.Options {
				if err != nil {
					break
				}

				for _, prefix := range prefixes {
					if strings.HasPrefix(opt, prefix) {
						dirs := strings.Split(opt[len(prefix):], ":")
						for _, dir := range dirs {
							if _, err = os.Lstat(dir); err != nil {
								break
							}
						}
						break
					}
				}
			}

			if err != nil {
				break
			}

			continue
		}

		if _, err = os.Lstat(m.Source); err != nil {
			break
		}
	}

	if err != nil {
		var b strings.Builder
		b.Grow(256)
		b.WriteString("src:")
		b.WriteString(err.Error())
		return b.String()
	}

	if _, err = os.Lstat(target); err != nil {
		var b strings.Builder
		b.Grow(256)
		b.WriteString("mountpoint:")
		b.WriteString(err.Error())
		return b.String()
	}

	return ""
}

func (m *mounter) Unmount(ctx context.Context, volumeId, target string) (err error) {
	c, err := containerd.New(m.containerdEndpoint, containerd.WithDefaultNamespace(m.namespace))
	if err != nil {
		klog.Fatalf("containerd connection is broken because the mounted unix socket somehow dose not work,"+
			"recreate the container may fix: %s", err)
	}

	if err = mount.UnmountAll(target, 0); err != nil {
		klog.Errorf("fail to unmount %s: %s", target, err)
		return err
	}

	klog.Infof("%s unmounted", target)

	if err = m.unrefSnapshot(ctx, c, volumeId, target); err != nil {
		klog.Errorf("fail to unref snapshot of volume %s: %s", volumeId, err)
		return
	}

	return
}
