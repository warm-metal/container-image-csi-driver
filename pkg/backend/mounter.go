package backend

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"k8s.io/klog/v2"
	k8smount "k8s.io/utils/mount"
)

type SnapshotMounter struct {
	runtime ContainerRuntime

	guard sync.Mutex
	// mapping from targets to key of read-only snapshots
	targetRoSnapshotMap map[MountTarget]SnapshotKey
	// reference counter of read-only snapshots
	roSnapshotTargetsMap map[SnapshotKey]map[MountTarget]struct{}
}

func NewMounter(runtime ContainerRuntime) *SnapshotMounter {
	mounter := &SnapshotMounter{
		runtime:              runtime,
		targetRoSnapshotMap:  make(map[MountTarget]SnapshotKey),
		roSnapshotTargetsMap: make(map[SnapshotKey]map[MountTarget]struct{}),
	}

	mounter.buildSnapshotCacheOrDie()
	return mounter
}

func (s *SnapshotMounter) buildSnapshotCacheOrDie() {
	// FIXME the timeout can be a flag.
	ctx, cancel := context.WithTimeout(context.TODO(), 20*time.Second)
	defer cancel()

	snapshots, err := s.runtime.ListSnapshots(ctx)
	if err != nil {
		klog.Fatalf("unable to list snapshots: %s", err)
	}

	klog.Infof("load %d snapshots from runtime", len(snapshots))

	mounter := k8smount.New("")

	s.guard.Lock()
	defer s.guard.Unlock()
	for _, metadata := range snapshots {
		key := metadata.GetSnapshotKey()
		if key == "" {
			klog.Fatalf("found a snapshot with a empty key")
		}

		if len(s.roSnapshotTargetsMap[key]) > 0 {
			klog.Fatalf("another snapshot with key %q has already been loaded", key)
		}

		targets := metadata.GetTargets()
		if len(targets) == 0 {
			klog.Fatalf("snapshot %q doesn't have a targets", key)
		}

		numTargetsLoaded := len(targets)
		for target := range targets {
			// FIXME Considering using checksum of target instead to shorten metadata.
			// But the mountpoint checking become unavailable any more.
			if notMount, err := mounter.IsLikelyNotMountPoint(string(target)); err != nil || notMount {
				klog.Errorf("target %q is not a mountpoint yet. trying to release the ref of snapshot %q",
					key)
				delete(targets, target)
				continue
			}

			s.targetRoSnapshotMap[target] = key
			klog.Infof("snapshot %q mounted to %s", key, target)
		}

		if len(targets) > 0 {
			if len(targets) != numTargetsLoaded {
				klog.Infof("some targets of snapshot %q changed, update metadata", key)
				if err := s.runtime.UpdateSnapshotMetadata(ctx, key, buildSnapshotMetaData(targets)); err != nil {
					klog.Fatalf("unable to update metadata of snapshot %q: %s", key, err)
				}
			}

			s.roSnapshotTargetsMap[key] = targets
		} else {
			klog.Infof("snapshot %q doesn't have any mounts. delete!", key)
			if err := s.runtime.DestroySnapshot(ctx, key); err != nil {
				klog.Fatalf("unable to destroy snapshot %q: %s", key, err)
			}
		}
	}
}

func (s *SnapshotMounter) refROSnapshot(
	ctx context.Context, target MountTarget, imageID string, key SnapshotKey, metadata SnapshotMetadata,
) (err error) {
	s.guard.Lock()
	defer s.guard.Unlock()

	if s.targetRoSnapshotMap[target] != "" {
		klog.Fatalf("target %q has already been mounted to snapshot %q", target, s.targetRoSnapshotMap[target])
	}

	if len(s.roSnapshotTargetsMap[key]) > 0 {
		klog.Infof("snapshot %q has already been used by other volumes. update its metadata to refer", key)
		metadata.CopyTargets(s.roSnapshotTargetsMap[key])
		if err := s.runtime.UpdateSnapshotMetadata(ctx, key, metadata); err != nil {
			return err
		}
	} else {
		klog.Infof("create snapshot %q of image %q and refer it", key, imageID)
		if err := s.runtime.PrepareReadOnlySnapshot(ctx, imageID, key, metadata); err != nil {
			return err
		}
		s.roSnapshotTargetsMap[key] = map[MountTarget]struct{}{}
	}

	s.roSnapshotTargetsMap[key][target] = struct{}{}
	s.targetRoSnapshotMap[target] = key
	klog.Infof("snapshot %q is shared by %d volumes", key, len(s.roSnapshotTargetsMap[key]))
	return nil
}

func (s *SnapshotMounter) unrefROSnapshot(ctx context.Context, target MountTarget) (found bool) {
	s.guard.Lock()
	defer s.guard.Unlock()

	key := s.targetRoSnapshotMap[target]
	if key == "" {
		klog.Infof("target %q is not read-only", target)
		return false
	}

	targets := s.roSnapshotTargetsMap[key]
	if len(targets) > 1 {
		delete(targets, target)
		klog.Infof("snapshot %q is also used by other volumes. update its metadata", key)
		if err := s.runtime.UpdateSnapshotMetadata(ctx, key, buildSnapshotMetaData(targets)); err != nil {
			klog.Fatalf("unable to update snapshot %q to unref it: %s. We will crash. The snapshot will be "+
				"updated when restarting", key, err)
		}
		delete(s.targetRoSnapshotMap, target)
		return true
	}

	if len(targets) == 0 {
		klog.Fatalf("refcount of snapshot %q is 0", key)
	}

	klog.Infof("snapshot %q isn't used by other volumes. delete it", key)
	if err := s.runtime.DestroySnapshot(ctx, key); err != nil {
		klog.Fatalf("unable to destroy snapshot %q: %s. We will crash. Dangling snapshots will be destroyed "+
			"when restarting", key, err)
	}

	delete(s.roSnapshotTargetsMap, key)
	delete(s.targetRoSnapshotMap, target)
	return true
}

func (s *SnapshotMounter) Mount(
	ctx context.Context, volumeId string, target MountTarget, image docker.Named, ro bool) (err error) {
	var key SnapshotKey
	imageID := s.runtime.GetImageIDOrDie(ctx, image)
	if ro {
		// Use the image ID as the key of the read-only snapshot
		if imageID == "" {
			klog.Fatalf("invalid image id of image %q", image)
		}

		key = genSnapshotKey(imageID)
		klog.Infof("refer read-only snapshot of image %q with key %q", image, key)
		if err := s.refROSnapshot(ctx, target, imageID, key, createSnapshotMetaData(target)); err != nil {
			return err
		}

		defer func() {
			if err != nil {
				klog.Infof("unref read-only snapshot because of error %s", err)
				if !s.unrefROSnapshot(ctx, target) {
					klog.Fatalf("target %q not found in the snapshot cache", target)
				}
			}
		}()
	} else {
		// For read-write volumes, they must be ephemeral volumes, that which volumeIDs are unique strings.
		key = genSnapshotKey(volumeId)
		klog.Infof("create read-write snapshot of image %q with key %q", image, key)
		if err := s.runtime.PrepareRWSnapshot(ctx, imageID, key, nil); err != nil {
			return err
		}

		defer func() {
			if err != nil {
				klog.Infof("unref read-write snapshot because of error %s", err)
				s.runtime.DestroySnapshot(ctx, key)
			}
		}()
	}

	err = s.runtime.Mount(ctx, key, target, ro)
	return err
}

func (s *SnapshotMounter) Unmount(ctx context.Context, volumeId string, target MountTarget) error {
	klog.Infof("unmount volume %q at %q", volumeId, target)
	if err := s.runtime.Unmount(ctx, target); err != nil {
		return err
	}

	klog.Infof("try to unref read-only snapshot")
	// Try to unref a read-only snapshot.
	if s.unrefROSnapshot(ctx, target) {
		return nil
	}

	klog.Infof("delete the read-write snapshot")
	// Must be a read-write snapshot
	return s.runtime.DestroySnapshot(ctx, genSnapshotKey(volumeId))
}

func (s *SnapshotMounter) ImageExists(ctx context.Context, image docker.Named) bool {
	return s.runtime.ImageExists(ctx, image)
}

func genSnapshotKey(parent string) SnapshotKey {
	return SnapshotKey(fmt.Sprintf("csi-image.warm-metal.tech-%s", parent))
}
