package containerd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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

type Options struct {
	SocketPath     string
	MountRate      int
	UmountRate     int
	MountBurst     int
	UmountBurst    int
	StartupTimeout time.Duration
}

func NewMounter(o *Options) backend.Mounter {
	c, err := containerd.New(o.SocketPath, containerd.WithDefaultNamespace("k8s.io"))
	if err != nil {
		klog.Fatalf("containerd connection is broken because the mounted unix socket somehow does not work,"+
			"recreate the container may fix: %s", err)
	}

	return NewContainerdMounter(&snapshotMounter{
		snapshotter:   c.SnapshotService(""),
		leasesService: c.LeasesService(),
		cli:           c,
	}, o)
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
	return s.getImageIDOrDieByName(ctx, image.String())
}

func (s snapshotMounter) getImageIDOrDieByName(ctx context.Context, image string) string {
	localImage, err := s.cli.GetImage(ctx, image)
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
	ctx context.Context, image string, key backend.SnapshotKey, _ backend.SnapshotMetadata,
) error {
	labels := defaultSnapshotLabels()

	parent := s.getImageIDOrDieByName(ctx, image)

	klog.Infof("create ro snapshot %q for image %q with metadata %#v", key, image, labels)
	_, err := s.FindSnapshot(ctx, string(key), parent, snapshots.KindView, labels)
	if err != nil {
		return err
	}

	if _, err = s.snapshotter.View(ctx, string(key), parent, snapshots.WithLabels(labels)); err != nil {
		klog.Errorf("unable to create read-only snapshot %q of image %q: %s", key, image, err)
	}

	return err
}

func (s snapshotMounter) PrepareRWSnapshot(
	ctx context.Context, image string, key backend.SnapshotKey, _ backend.SnapshotMetadata,
) error {
	labels := defaultSnapshotLabels()
	parent := s.getImageIDOrDieByName(ctx, image)

	klog.Infof("create rw snapshot %q for image %q with metadata %#v", key, parent, labels)
	_, err := s.FindSnapshot(ctx, string(key), parent, snapshots.KindActive, labels)
	if err != nil {
		return err
	}

	if _, err = s.snapshotter.Prepare(ctx, string(key), parent, snapshots.WithLabels(labels)); err != nil {
		klog.Errorf("unable to create snapshot %q of image %q: %s", key, parent, err)
	}

	return err
}

func (s snapshotMounter) FindSnapshot(
	ctx context.Context, key, parent string, kind snapshots.Kind, labels map[string]string,
) (*snapshots.Info, error) {
	stat, err := s.snapshotter.Stat(ctx, key)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			// this is the expected case of this function, all other are cases are errors
			return nil, nil
		}
		return nil, err
	}

	if stat.Kind == kind && stat.Parent == parent {
		extactMatch := true
		for k, v := range labels {
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

func (s snapshotMounter) MigrateOldSnapshotFormat(ctx context.Context) error {
	oldImages := make(map[*snapshots.Info]map[backend.MountTarget]struct{})
	err := s.snapshotter.Walk(ctx, func(ctx context.Context, info snapshots.Info) error {
		targets := make(map[backend.MountTarget]struct{}, len(info.Labels))
		infoP := &info
		for k := range info.Labels {
			// To be compatible with old snapshots(prior to v0.4.2), we must filter read-write snapshots out.
			// The read-write snapshot always has a key of leading with "csi-", while the key of a read-only snapshot
			// is its image ID.
			if strings.HasPrefix(k, volumeIdLabelPrefix) {
				if strings.HasPrefix(info.Name[len(labelPrefix)+1:], "csi-") {
					klog.Infof("rw snapshot %q with labels %#v is created by an old versioned driver, skip it",
						info.Name, info.Labels)
					targets = nil
					break
				}

				if _, err := docker.ParseNamed(info.Name[len(labelPrefix)+1:]); err == nil {
					klog.Warningf("snapshot %q with labels %#v is an old versioned snapshot used by a PV. "+
						"It will be excluded from the ro snapshot cache, but it still can be unmounted normally.",
						info.Name, info.Labels)
					targets = nil
					break
				}
			}

			if strings.HasPrefix(k, targetLabelPrefix) {
				targets[backend.MountTarget(k[len(targetLabelPrefix)+1:])] = struct{}{}
			}
		}

		if len(targets) > 0 {
			oldImages[infoP] = targets
		}

		return nil
	}, "labels.\""+typeLabel+"\"!=lease-only")

	if err != nil {
		klog.Errorf("unable to list snapshots for migration: %s", err)
		return err
	}

	for oldImage, targets := range oldImages {
		for target := range targets {
			s.leasesService.Create(ctx, leases.WithID(string(target)), leases.WithLabels(defaultSnapshotLabels()))
			s.leasesService.AddResource(ctx, leases.Lease{ID: string(target)}, leases.Resource{
				Type: "snapshots/overlayfs",
				ID:   oldImage.Name,
			})
			klog.Infof("migrated target %q for snapshot %q by adding lease", target, oldImage.Name)
		}
		oldImage.Labels = defaultSnapshotLabels()
		_, err := s.snapshotter.Update(ctx, *oldImage)
		if err != nil {
			klog.Errorf("unable to update snapshot %q: %s", oldImage.Name, err)
			return err
		}
		klog.Infof("snapshot %q migrated", oldImage.Name)
	}

	return err
}

func (s snapshotMounter) ListSnapshots(ctx context.Context) ([]backend.SnapshotMetadata, error) {
	return s.ListSnapshotsWithFilter(ctx, managedFilter)
}

func (s snapshotMounter) ListSnapshotsWithFilter(ctx context.Context, filters ...string) ([]backend.SnapshotMetadata, error) {
	var ss []backend.SnapshotMetadata

	allLeases, err := s.leasesService.List(ctx, managedFilter)
	if err != nil {
		klog.Errorf("unable to list leases: %s", err)
		return nil, err
	}

	resourceToLeases := make(map[string]map[backend.MountTarget]struct{})
	for _, l := range allLeases {
		res, err := s.leasesService.ListResources(ctx, l)
		if err != nil {
			klog.Errorf("unable to list resources of lease %q: %s", l.ID, err)
			return nil, err
		}
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

		targets := resourceToLeases[info.Name]
		metadata := make(backend.SnapshotMetadata)
		metadata.SetSnapshotKey(info.Name)
		metadata.SetTargets(targets)
		ss = append(ss, metadata)
		klog.Infof("got ro snapshot %q with %d targets %#v using filter %#v", info.Name, len(targets), targets, filters)

		return nil
	}, filters...)

	if err != nil {
		klog.Errorf("unable to list snapshots: %s", err)
		return nil, err
	}

	return ss, nil
}

const (
	labelPrefix   = "csi-image.warm-metal.tech"
	typeLabel     = labelPrefix + "/type"
	managedFilter = "labels.\"" + typeLabel + "\"==lease-only"

	// old format labels
	volumeIdLabelPrefix = labelPrefix + "/id"
	targetLabelPrefix   = labelPrefix + "/target"
	gcLabel             = "containerd.io/gc.root"
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
