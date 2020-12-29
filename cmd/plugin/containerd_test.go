package main

import (
	"context"
	"flag"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/reference/docker"
	"os"
	"path/filepath"
	"testing"
)

func init() {
	flag.Set("logtostderr", "true")
}

func pullImage(image string) error {
	c, err := containerd.New(*containerdSock, containerd.WithDefaultNamespace(*defaultContainerdNamespace))
	if err != nil {
		return err
	}

	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		return err
	}

	_, err = c.Pull(context.TODO(), namedRef.String(), containerd.WithPullUnpack, containerd.WithSchema1Conversion)
	return err
}

func removeImage(image string) error {
	c, err := containerd.New(*containerdSock, containerd.WithDefaultNamespace(*defaultContainerdNamespace))
	if err != nil {
		return err
	}

	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		return err
	}

	return c.ImageService().Delete(context.TODO(), namedRef.String(), images.SynchronousDelete())
}

func testMountAndUmount(t *testing.T, id, image, target string) {
	if image != "warmmetal/csi-image-test:simple-fs" {
		t.Fatalf(`"image" must be ""warmmetal/csi-image-test:simple-fs""`)
	}

	if err := os.MkdirAll(target, 0750); err != nil {
		t.Error(err)
		t.Fail()
	}

	if err := mountContainerdImage(context.TODO(), id, image, target); err != nil {
		t.Error(err)
		t.Fail()
	}

	defer func() {
		if err := umountContainerdImage(context.TODO(), id, target); err != nil {
			t.Error(err)
			t.Fail()
		}
	}()

	if fi, err := os.Lstat(filepath.Join(target, "csi-folder1")); err != nil || !fi.IsDir() {
		t.Error(err)
		t.Fail()
	}

	if fi, err := os.Lstat(filepath.Join(target, "csi-file1")); err != nil || fi.IsDir() {
		t.Error(err)
		t.Fail()
	}

	if fi, err := os.Lstat(filepath.Join(target, "csi-file2")); err != nil || fi.IsDir() {
		t.Error(err)
		t.Fail()
	}
}

func TestLocalMountAndUmount(t *testing.T) {
	id := "test-volume-id"
	image := "warmmetal/csi-image-test:simple-fs"
	target := "/tmp/image-mount-point/simple-fs"
	*defaultContainerdNamespace = "buildkit"

	if err := pullImage(image); err != nil {
		t.Fatal(err)
	}

	testMountAndUmount(t, id, image, target)
}

func TestRemoteMountAndUmount(t *testing.T) {
	id := "test-volume-id"
	image := "warmmetal/csi-image-test:simple-fs"
	target := "/tmp/image-mount-point/simple-fs"
	*defaultContainerdNamespace = "buildkit"

	if err := removeImage(image); err != nil {
		t.Fatal(err)
	}

	testMountAndUmount(t, id, image, target)
}

func TestMountTheSameImage(t *testing.T) {
	idFoo := "volume-id-foo"
	idBar := "volume-id-bar"
	image := "warmmetal/csi-image-test:simple-fs"
	targetFoo := "/tmp/image-mount-point/foo"
	targetBar := "/tmp/image-mount-point/bar"
	testMountAndUmount(t, idFoo, image, targetFoo)
	testMountAndUmount(t, idBar, image, targetBar)
}

func TestMountTheSameVolume(t *testing.T) {
	id := "volume-id"
	image := "warmmetal/csi-image-test:simple-fs"
	targetFoo := "/tmp/image-mount-point/foo"
	targetBar := "/tmp/image-mount-point/bar"
	testMountAndUmount(t, id, image, targetFoo)
	testMountAndUmount(t, id, image, targetBar)
}
