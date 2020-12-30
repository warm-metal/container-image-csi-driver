package containerd

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

const (
	containerdSock = "unix:///var/run/containerd/containerd.sock"
	defaultContainerdNamespace = "buildkit"
)

func pullImage(image string) error {
	c, err := containerd.New(containerdSock, containerd.WithDefaultNamespace(defaultContainerdNamespace))
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
	c, err := containerd.New(containerdSock, containerd.WithDefaultNamespace(defaultContainerdNamespace))
	if err != nil {
		return err
	}

	namedRef, err := docker.ParseDockerRef(image)
	if err != nil {
		return err
	}

	return c.ImageService().Delete(context.TODO(), namedRef.String(), images.SynchronousDelete())
}

func testMountAndUmount(t *testing.T, image, target string, unmount bool) {
	if image != "warmmetal/csi-image-test:simple-fs" {
		t.Fatalf(`"image" must be ""warmmetal/csi-image-test:simple-fs""`)
	}

	if err := os.MkdirAll(target, 0750); err != nil {
		t.Error(err)
		t.Fail()
	}

	m := NewMounter(containerdSock, defaultContainerdNamespace)
	if err := m.Mount(context.TODO(), image, target); err != nil {
		t.Error(err)
		t.Fail()
	}

	if unmount {
		defer func() {
			if err := m.Unmount(context.TODO(), image, target); err != nil {
				t.Error(err)
				t.Fail()
			}
		}()
	}

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
	image := "warmmetal/csi-image-test:simple-fs"
	target := "/tmp/image-mount-point/simple-fs"

	if err := pullImage(image); err != nil {
		t.Fatal(err)
	}

	testMountAndUmount(t, image, target, true)
}

func TestRemoteMountAndUmount(t *testing.T) {
	image := "warmmetal/csi-image-test:simple-fs"
	target := "/tmp/image-mount-point/simple-fs"

	if err := removeImage(image); err != nil {
		t.Fatal(err)
	}

	testMountAndUmount(t, image, target, true)
}

func TestMountTheSameImage(t *testing.T) {
	image := "warmmetal/csi-image-test:simple-fs"
	targetFoo := "/tmp/image-mount-point/foo"
	targetBar := "/tmp/image-mount-point/bar"
	testMountAndUmount(t, image, targetFoo, false)
	testMountAndUmount(t, image, targetBar, true)

	m := NewMounter(containerdSock, defaultContainerdNamespace)
	if err := m.Unmount(context.TODO(), image, targetFoo); err != nil {
		t.Error(err)
		t.Fail()
	}
}
