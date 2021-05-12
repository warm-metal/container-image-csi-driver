package backend

import (
	"context"
	"github.com/containerd/containerd/reference/docker"
)

type MountOptions struct {
	ReadOnly   bool
}

type SnapshotKey string
type MountTarget string

type ContainerRuntime interface {
	Mount(ctx context.Context, key SnapshotKey, target MountTarget, ro bool) error
	Unmount(ctx context.Context, target MountTarget) error

	// Determines if a local image exists. A false should return if errors arise.
	ImageExists(ctx context.Context, image docker.Named) bool

	// Retrieves the image ID of a local image.
	// It should crash on any failures including not local image found.
	GetImageIDOrDie(ctx context.Context, image docker.Named) string

	// Create a snapshot of the image using the given key and metadata.
	// It should throw errors if any snapshot exists with the same key.
	PrepareReadOnlySnapshot(ctx context.Context, imageID string, key SnapshotKey, metadata SnapshotMetadata) error

	// Create a read-write snapshot of the image using the given key and metadata.
	// It should throw errors if any snapshot exists with the same key.
	PrepareRWSnapshot(ctx context.Context, imageID string, key SnapshotKey, metadata SnapshotMetadata) error

	// Replace the metadata of the snapshot with the specified key with the given metadata.
	UpdateSnapshotMetadata(ctx context.Context, key SnapshotKey, metadata SnapshotMetadata) error

	// Destroy the snapshot with the given key.
	// It should throw errors if the snapshot doesn't exist.
	DestroySnapshot(ctx context.Context, key SnapshotKey) error

	// List metadata of all snapshots created by the driver.
	// The snapshot key must also be saved in the returned map with the key "FakeMetaDataSnapshotKey".
	ListSnapshots(ctx context.Context) ([]SnapshotMetadata, error)
}
