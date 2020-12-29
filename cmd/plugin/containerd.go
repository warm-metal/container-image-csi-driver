package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/snapshots"
	"github.com/golang/glog"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
	"time"
)

var (
	containerdSock = flag.String(
		"containerd-addr", "unix:///var/run/containerd/containerd.sock", "endpoint of containerd")
	defaultContainerdNamespace = flag.String(
		"containerd-default-namespace", "docker",
		`the default namespace containerd used in the cluster. It usually is "docker" if docker is used as runtime, or "k8s" if CRI is used.`)
)

func genSnapshotKey(volumeID, targetPath string) string {
	return fmt.Sprintf("csi-image.warm-metal.tech-%s-%s", volumeID, targetPath)
}

func mountContainerdImage(ctx context.Context, id, image, targetPath string) (err error) {
	c, err := containerd.New(*containerdSock, containerd.WithDefaultNamespace(*defaultContainerdNamespace))
	if err != nil {
		glog.Errorf("fail to create containerd client: %s", err)
		return
	}

	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		glog.Errorf("fail to normalize image: %s, %s", image, err)
		return err
	}

	localImage, err := c.GetImage(ctx, namedRef.String())
	if err != nil {
		if errors.Cause(err) != errdefs.ErrNotFound {
			glog.Errorf("fail to retrieve local image: %s, %s", namedRef, err)
			return
		}

		glog.Infof("no local image found. Pull %s", namedRef)
		localImage, err = c.Pull(ctx, namedRef.String(), containerd.WithPullUnpack, containerd.WithSchema1Conversion)
		if err != nil {
			glog.Errorf("fail to pull image: %s, %s", namedRef, err)
			return err
		}
	} else {
		glog.Infof("found local image %s", namedRef)
	}

	snapshotterName := containerd.DefaultSnapshotter
	if err = localImage.Unpack(ctx, snapshotterName); err != nil {
		glog.Errorf("fail to unpack image: %s, %s, %s", namedRef, snapshotterName, err)
		return
	}

	glog.Infof("image %s unpacked", namedRef)
	diffIDs, err := localImage.RootFS(ctx)
	if err != nil {
		return
	}

	glog.Infof("rootfs of image %s : %#v", namedRef, diffIDs)
	parent := identity.ChainID(diffIDs).String()
	glog.Infof("prepare %s", parent)
	key := genSnapshotKey(id, targetPath)
	snapshotter := c.SnapshotService(snapshotterName)
	mounts, err := snapshotter.View(ctx, key, parent, snapshots.WithLabels(map[string]string{
		"containerd.io/gc.root": time.Now().UTC().Format(time.RFC3339),
	}))
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			mounts, err = snapshotter.Mounts(ctx, key)
		}

		if err != nil {
			glog.Errorf("fail to prepare image: %s, %s, %s, %s", parent, id, snapshotterName, err)
			return
		}
	} else {
		defer func() {
			if err != nil {
				glog.Errorf("found error %s. Prepare removing the snapshot just created", err)
				if err := snapshotter.Remove(ctx, key); err != nil {
					glog.Errorf("fail to recycle snapshot: %s, %s", id, err)
				}
			}
		}()
	}

	err = mount.All(mounts, targetPath)
	if err != nil {
		glog.Errorf("fail to mount image %s to %s: %s", namedRef, targetPath, err)
	} else {
		glog.Infof("image %s mounted", namedRef)
	}

	return err
}

func umountContainerdImage(ctx context.Context, id, targetPath string) (err error) {
	c, err := containerd.New(*containerdSock, containerd.WithDefaultNamespace(*defaultContainerdNamespace))
	if err != nil {
		glog.Errorf("fail to create containerd client: %s", err)
		return
	}

	if err = mount.UnmountAll(targetPath, 0); err != nil {
		glog.Errorf("fail to unmount %s: %s", targetPath, err)
		return err
	}

	glog.Infof("%s unmounted", targetPath)

	snapshotterName := containerd.DefaultSnapshotter
	key := genSnapshotKey(id, targetPath)
	err = c.SnapshotService(snapshotterName).Remove(ctx, key)
	if err != nil {
		glog.Errorf("fail to rm snapshot %s: %s", key, err)
		return
	}

	glog.Infof("snapshot %s removed", key)
	return
}
