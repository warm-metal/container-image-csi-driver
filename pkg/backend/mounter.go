package backend

import "context"

type Mounter interface {
    Mount(ctx context.Context, image, target string) error
    Unmount(ctx context.Context, image, target string) error
}
