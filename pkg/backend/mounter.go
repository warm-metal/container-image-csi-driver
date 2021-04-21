package backend

import (
	"context"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
)

type MountOptions struct {
	PullAlways bool
	ReadOnly   bool
	VolumeId   string
}

type Mounter interface {
	Mount(ctx context.Context, puller remoteimage.Puller, volumeId, image, target string, opts *MountOptions) error
	Unmount(ctx context.Context, volumeId, target string) error
}
