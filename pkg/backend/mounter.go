package backend

import "context"

type Mounter interface {
    Mount(ctx context.Context, volumeId, image, target string) error
    Unmount(ctx context.Context, volumeId, target string) error
}
