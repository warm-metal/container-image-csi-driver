package containerd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/leases"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/snapshots"
	"github.com/opencontainers/image-spec/identity"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"k8s.io/klog/v2"
)

type snapshotMounter struct {
	snapshotter   snapshots.Snapshotter
	leasesService leases.Manager
	cli           *containerd.Client
}

func NewMounter(socketPath string) backend.Mounter {
	c, err := containerd.New(socketPath, containerd.WithDefaultNamespace("k8s.io"))
	if err != nil {
		klog.Fatalf("containerd connection is broken because the mounted unix socket somehow does not work,"+
			"recreate the container may fix: %s", err)
	}

	return NewMounter2(&snapshotMounter{
		snapshotter:   c.SnapshotService(""),
		leasesService: c.LeasesService(),
		cli:           c,
	})
}

func (s snapshotMounter) Mount(ctx context.Context, key backend.SnapshotKey, target backend.MountTarget, ro bool) error {
	mounts, err := s.snapshotter.Mounts(ctx, string(key))
	if err != nil {
		klog.Errorf("unable to retrieve mounts of snapshot %q: %s", key, err)
		return err
	}

	err = mount.All(mounts, string(target))
	if err != nil {
		mountsErr := describeMounts(mounts, string(target))
		if len(mountsErr) > 0 {
			err = errors.New(mountsErr)
		}

		klog.Errorf("unable to mount snapshot %q to target %s: %s", key, target, err)
	}

	return err
}

func (s snapshotMounter) Unmount(_ context.Context, target backend.MountTarget) error {
	if err := mount.UnmountAll(string(target), 0); err != nil {
		klog.Errorf("fail to unmount %s: %s", target, err)
		return err
	}
	return nil
}

func (s snapshotMounter) ImageExists(ctx context.Context, image docker.Named) bool {
	_, err := s.cli.GetImage(ctx, image.String())
	return err == nil
}

func (s snapshotMounter) GetImageIDOrDie(ctx context.Context, image docker.Named) string {
	localImage, err := s.cli.GetImage(ctx, image.String())
	if err != nil {
		klog.Fatalf("unable to retrieve local image %q: %s", image, err)
	}

	if err = localImage.Unpack(ctx, ""); err != nil {
		klog.Fatalf("unable to unpack image %q: %s", image, err)
	}
	klog.Infof("image %q unpacked", image)

	diffIDs, err := localImage.RootFS(ctx)
	if err != nil {
		klog.Fatalf("unable to fetch rootfs of image %q: %s", image, err)
	}

	return identity.ChainID(diffIDs).String()
}

func (s snapshotMounter) AddLeaseToContext(ctx context.Context, target string) (context.Context, error) {
	l, err := s.leasesService.Create(ctx, leases.WithID(target), leases.WithLabels(defaultSnapshotLabels()))
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			klog.Infof("lease %q already exists", target)
			l = leases.Lease{ID: target}
		} else {
			klog.Errorf("unable to create lease %q: %s", target, err)
			return nil, err
		}
	} else {
		klog.Infof("lease %q added", l.ID)
	}

	leaseCtx := leases.WithLease(ctx, l.ID)

	return leaseCtx, nil
}

func (s snapshotMounter) RemoveLease(ctx context.Context, target string) error {
	l := leases.Lease{
		ID: target,
	}

	if err := s.leasesService.Delete(ctx, l); err != nil {
		klog.Errorf("unable to delete lease %q: %s", target, err)
		return err
	}

	klog.Infof("lease %q removed", target)
	return nil
}

func (s snapshotMounter) PrepareReadOnlySnapshot(
	ctx context.Context, imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	labels := defaultSnapshotLabels()

	klog.Infof("create ro snapshot %q for image %q with metadata %#v", key, imageID, labels)
	_, err := s.FindSnapshot(ctx, string(key), imageID, snapshots.KindView, labels)
	if err != nil {
		return err
	}

	if _, err = s.snapshotter.View(ctx, string(key), imageID, snapshots.WithLabels(labels)); err != nil {
		klog.Errorf("unable to create read-only snapshot %q of image %q: %s", key, imageID, err)
	}

	return err
}

func (s snapshotMounter) PrepareRWSnapshot(
	ctx context.Context, imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	labels := defaultSnapshotLabels()

	klog.Infof("create rw snapshot %q for image %q with metadata %#v", key, imageID, labels)
	_, err := s.FindSnapshot(ctx, string(key), imageID, snapshots.KindActive, labels)
	if err != nil {
		return err
	}

	if _, err = s.snapshotter.Prepare(ctx, string(key), imageID, snapshots.WithLabels(labels)); err != nil {
		klog.Errorf("unable to create snapshot %q of image %q: %s", key, imageID, err)
	}

	return err
}

func (s snapshotMounter) FindSnapshot(
	ctx context.Context, key, parent string, kind snapshots.Kind, labels map[string]string,
) (*snapshots.Info, error) {
	stat, err := s.snapshotter.Stat(ctx, key)
	if err != nil {
		return nil, err
	}

	if stat.Kind == kind && stat.Parent == parent {
		extactMatch := true
		for k, v := range labels {
			if k == "containerd.io/gc.root" {
				continue
			}

			if k == "csi-image.warm-metal.tech/target" {
				continue
			}

			if stat.Labels[k] != v {
				extactMatch = false
				break
			}
		}

		if extactMatch {
			return &stat, fmt.Errorf("snapshot %q already exists with exact match", key)
		}
	}

	return &stat, fmt.Errorf("snapshot %q already exists with different configuration %v", key, stat)
}

func (s snapshotMounter) UpdateSnapshotMetadata(
	ctx context.Context, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	if l, ok := leases.FromContext(ctx); ok {
		err := s.leasesService.AddResource(ctx, leases.Lease{ID: l}, leases.Resource{
			Type: "snapshots/overlayfs",
			ID:   string(key),
		})

		if err != nil {
			klog.Errorf("unable to add resource %q to lease %q: %s", string(key), l, err)
			return err
		}

		klog.Infof("resource %q added to lease %q", string(key), l)
	} else {
		klog.Errorf("lease is not found in context")
	}
	return nil
}

func (s snapshotMounter) DestroySnapshot(ctx context.Context, key backend.SnapshotKey) error {
	return nil
}

func (s snapshotMounter) ListSnapshots(ctx context.Context) ([]backend.SnapshotMetadata, error) {
	var ss []backend.SnapshotMetadata

	resourceToLeases := make(map[string]map[backend.MountTarget]struct{})

	// todo: add migration of previous format without lease

	allLeases, err := s.leasesService.List(ctx)
	if err != nil {
		klog.Errorf("unable to list leases: %s", err)
		return nil, err
	}

	for _, l := range allLeases {
		if l.Labels[typeLabel] != "lease-only" {
			klog.Info("skip lease %q", l.ID)
			continue
		}

		res, _ := s.leasesService.ListResources(ctx, l)
		for _, r := range res {
			if (r.Type != "snapshots/overlayfs") || (r.ID == "") {
				continue
			}
			if _, ok := resourceToLeases[r.ID]; !ok {
				resourceToLeases[r.ID] = make(map[backend.MountTarget]struct{})
			}
			resourceToLeases[r.ID][backend.MountTarget(l.ID)] = struct{}{}
		}
	}

	err = s.snapshotter.Walk(ctx, func(ctx context.Context, info snapshots.Info) error {
		if len(resourceToLeases[info.Name]) == 0 {
			return nil
		}

		managedSnapshot := true
		for key, value := range info.Labels {
			if key == typeLabel && value == "lease-only" {
				// We only care about the snapshots created by the driver itself.
				managedSnapshot = true
				break
			}
		}

		if !managedSnapshot {
			klog.Infof("skip snapshot %q", info.Name)
			return nil
		}

		targets := resourceToLeases[info.Name]
		metadata := make(backend.SnapshotMetadata)
		metadata.SetSnapshotKey(info.Name)
		metadata.SetTargets(targets)
		ss = append(ss, metadata)
		klog.Infof("got ro snapshot %q with %d targets %#v", info.Name, len(targets), targets)

		return nil
	})

	if err != nil {
		klog.Errorf("unable to list snapshots: %s", err)
		return nil, err
	}

	return ss, nil
}

const (
	labelPrefix = "csi-image.warm-metal.tech"
	typeLabel   = labelPrefix + "/type"
)

func defaultSnapshotLabels() map[string]string {
	return map[string]string{
		typeLabel: "lease-only",
	}
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
