package containerd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"golang.org/x/time/rate"
	"k8s.io/klog/v2"
	k8smount "k8s.io/utils/mount"
)

type SnapshotMounter struct {
	runtime       backend.ContainerRuntimeMounter
	guard         sync.Mutex
	mountlimiter  *rate.Limiter
	umountlimiter *rate.Limiter
}

func NewContainerdMounter(runtime backend.ContainerRuntimeMounter, o *Options) *SnapshotMounter {
	if o.MountRate <= 0 || o.MountBurst <= 0 || o.UmountRate <= 0 || o.UmountBurst <= 0 {
		klog.Fatalf("invalid rate or burst limit: %+v", o)
	}

	mounter := &SnapshotMounter{
		runtime: runtime,
		guard:   sync.Mutex{},
		// we need to limit the rate of mount and unmount to avoid the system being overwhelmed
		// because the mount operation is causing way more load than the unmount operation on containerd
		// we are using different limits for mount and unmount
		mountlimiter:  rate.NewLimiter(rate.Limit(o.MountRate), o.MountBurst),
		umountlimiter: rate.NewLimiter(rate.Limit(o.UmountRate), o.UmountBurst),
	}

	mounter.buildSnapshotCacheOrDie(o.StartupTimeout)
	return mounter
}

func (s *SnapshotMounter) buildSnapshotCacheOrDie(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	if err := s.runtime.MigrateOldSnapshotFormat(ctx); err != nil {
		klog.Fatalf("unable to migrate old snapshot format: %s", err)
	}

	snapshots, err := s.runtime.ListSnapshots(ctx)
	if err != nil {
		klog.Fatalf("unable to list snapshots: %s", err)
	}

	klog.Infof("load %d snapshots from runtime", len(snapshots))

	mounter := k8smount.New("")

	for _, metadata := range snapshots {
		key := metadata.GetSnapshotKey()
		if key == "" {
			klog.Fatalf("found a snapshot with a empty key")
		}

		for target := range metadata.GetTargets() {
			if notMount, err := mounter.IsLikelyNotMountPoint(string(target)); err != nil || notMount {
				klog.Errorf("target %q is not a mountpoint yet. trying to release the ref of snapshot %q",
					key)

				_ = s.runtime.RemoveLease(ctx, string(target))
				continue
			}

			klog.Infof("snapshot %q mounted to %s", key, target)
		}
	}
}

func (s *SnapshotMounter) refROSnapshot(
	ctx context.Context, _ backend.MountTarget, image string, key backend.SnapshotKey,
) (err error) {
	s.guard.Lock()
	defer s.guard.Unlock()

	currentSnapshots, err := s.runtime.ListSnapshotsWithFilter(ctx, "name==\""+string(key)+"\"")
	if err != nil {
		return err
	}
	snapshotExists := len(currentSnapshots) > 0
	if snapshotExists {
		return s.runtime.UpdateSnapshotMetadata(ctx, key, buildSnapshotMetaData())
	} else {
		return s.runtime.PrepareReadOnlySnapshot(ctx, image, key, buildSnapshotMetaData())
	}
}

func (s *SnapshotMounter) unrefROSnapshot(ctx context.Context, target backend.MountTarget) {
	s.runtime.RemoveLease(ctx, string(target))
}

func buildSnapshotMetaData() backend.SnapshotMetadata {
	return backend.SnapshotMetadata{}
}

func (s *SnapshotMounter) Mount(
	ctx context.Context, volumeId string, target backend.MountTarget, image docker.Named, ro bool) (err error) {

	r := s.mountlimiter.Reserve()
	if !r.OK() {
		return fmt.Errorf("not able to reserve rate limit")
	} else if r.Delay() > 0 {
		klog.Infof("rate limit reached during mount, waiting for %s", r.Delay())
		time.Sleep(r.Delay())
	}

	leaseCtx, err := s.runtime.AddLeaseToContext(ctx, string(target))
	if err != nil {
		return err
	}

	var key backend.SnapshotKey
	if ro {
		key = GenSnapshotKey(image.String())
		klog.Infof("refer read-only snapshot of image %q with key %q", image.String(), key)
		if err := s.refROSnapshot(leaseCtx, target, image.String(), key); err != nil {
			return err
		}

		defer func() {
			if err != nil {
				klog.Infof("unref read-only snapshot because of error %s", err)
				s.unrefROSnapshot(leaseCtx, target)
			}
		}()
	} else {
		
		// For read-write volumes, they must be ephemeral volumes, that which volumeIDs are unique strings.
		key = GenSnapshotKey(volumeId)
		klog.Infof("create read-write snapshot of image %q with key %q", image, key)
		if err := s.runtime.PrepareRWSnapshot(leaseCtx, image.String(), key, nil); err != nil {
			return err
		}

		defer func() {
			if err != nil {
				klog.Infof("unref read-write snapshot because of error %s", err)
				_ = s.runtime.RemoveLease(leaseCtx, string(target))
			}
		}()
	}

	err = s.runtime.Mount(leaseCtx, key, target, ro)
	return err
}

func (s *SnapshotMounter) Unmount(ctx context.Context, volumeId string, target backend.MountTarget) error {
	r := s.umountlimiter.Reserve()
	if !r.OK() {
		return fmt.Errorf("not able to reserve rate limit")
	} else if r.Delay() > 0 {
		klog.Infof("rate limit reached during umount, waiting for %s", r.Delay())
		time.Sleep(r.Delay())
	}

	klog.Infof("unmount volume %q at %q", volumeId, target)
	if err := s.runtime.Unmount(ctx, target); err != nil {
		return err
	}

	s.unrefROSnapshot(ctx, target)
	return nil
}

func (s *SnapshotMounter) ImageExists(ctx context.Context, image docker.Named) bool {
	return s.runtime.ImageExists(ctx, image)
}

func GenSnapshotKey(parent string) backend.SnapshotKey {
	return backend.SnapshotKey(fmt.Sprintf("csi-image.warm-metal.tech-%s", parent))
}
