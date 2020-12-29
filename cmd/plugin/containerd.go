package main

import (
	"context"
	"flag"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/reference/docker"
	"github.com/golang/glog"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
)

var (
	containerdSock = flag.String(
		"containerd-addr", "unix:///var/run/containerd/containerd.sock", "endpoint of containerd")
	defaultContainerdNamespace = flag.String(
		"containerd-default-namespace", "docker",
		`the default namespace containerd used in the cluster. It usually is "docker" if docker is used as runtime, or "k8s" if CRI is used.`)
)

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

		localImage, err = c.Pull(ctx, namedRef.String(), containerd.WithPullUnpack, containerd.WithSchema1Conversion)
		if err != nil {
			glog.Errorf("fail to pull image: %s, %s", namedRef, err)
			return err
		}
	}

	snapshotterName := containerd.DefaultSnapshotter
	//unpacked, err := localImage.IsUnpacked(ctx, snapshotterName)
	//if err != nil {
	//	glog.Errorf("fail to retrieve local image snapshotter: %s, %s, %s", namedRef, snapshotterName, err)
	//	return
	//}
	//
	//if unpacked {
	//
	//}

	glog.Infof("unpack image %s", namedRef)
	if err = localImage.Unpack(ctx, snapshotterName); err != nil {
		glog.Errorf("fail to unpack image: %s, %s, %s", namedRef, snapshotterName, err)
		return
	}

	diffIDs, err := localImage.RootFS(ctx)
	if err != nil {
		return
	}

	glog.Infof("rootfs of image %s : %#v", namedRef, diffIDs)
	parent := identity.ChainID(diffIDs).String()
	glog.Infof("prepare %s", parent)
	snapshotter := c.SnapshotService(snapshotterName)
	mounts, err := snapshotter.View(ctx, id, parent)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			mounts, err = snapshotter.Mounts(ctx, id)
		}

		if err != nil {
			glog.Errorf("fail to prepare image: %s, %s, %s, %s", parent, id, snapshotterName, err)
			return
		}
	}

	err = mount.All(mounts, targetPath)
	if err != nil {
		glog.Errorf("fail to mount image %s to %s: %s", namedRef, targetPath, err)
		if err := snapshotter.Remove(ctx, id); err != nil {
			glog.Errorf("fail to recycle snapshot: %s, %s", id, err)
		}
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
	err = c.SnapshotService(snapshotterName).Remove(ctx, id)
	if err != nil {
		glog.Errorf("fail to rm snapshot %s: %s", id, err)
		return
	}

	glog.Infof("snapshot %s removed", id)
	return
}
