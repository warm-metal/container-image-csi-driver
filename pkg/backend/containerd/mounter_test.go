package containerd

import (
	"context"
	"flag"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/reference/docker"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/cri"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func init() {
	flag.Set("logtostderr", "true")
}

const (
	containerdSock = "unix:///var/run/containerd/containerd.sock"
	defaultContainerdNamespace = "k8s.io"
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

func testMountAndUmount(t *testing.T, volumeId, image, target, secret string, unmount bool) {
	if image != "warmmetal/csi-image-test:simple-fs" && image != "kitt0hsu/private-image:simple-fs" {
		t.Fatalf(`"image" must be "warmmetal/csi-image-test:simple-fs" or "kitt0hsu/private-image:simple-fs"`)
	}

	if err := os.MkdirAll(target, 0750); err != nil {
		t.Error(err)
		t.Fail()
	}

	criClient, err := cri.NewRemoteImageService(containerdSock, time.Second)
	if err != nil {
		t.Error(err)
		t.Fail()
	}

	puller := remoteimage.NewPuller(criClient, image, secret, "default", "")
	m := NewMounter(containerdSock)
	if err := m.Mount(context.TODO(), puller, volumeId, image, target, &backend.MountOptions{}); err != nil {
		t.Error(err)
		t.Fail()
	}

	if unmount {
		defer func() {
			if err := m.Unmount(context.TODO(), volumeId, target); err != nil {
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

func TestLocalMountAndUmountAsPV(t *testing.T) {
	image := "warmmetal/csi-image-test:simple-fs"
	volumeId := image
	target := "/tmp/image-mount-point/simple-fs"

	if err := pullImage(image); err != nil {
		t.Fatal(err)
	}

	testMountAndUmount(t, volumeId, image, target, "",true)
}

func TestRemoteMountAndUmountAsPV(t *testing.T) {
	image := "warmmetal/csi-image-test:simple-fs"
	volumeId := image
	target := "/tmp/image-mount-point/simple-fs"

	if err := removeImage(image); err != nil {
		t.Fatal(err)
	}

	testMountAndUmount(t, volumeId, image, target, "",true)
}

func TestMountTheSameImageAsPV(t *testing.T) {
	image := "warmmetal/csi-image-test:simple-fs"
	volumeId := image
	targetFoo := "/tmp/image-mount-point/foo"
	targetBar := "/tmp/image-mount-point/bar"
	testMountAndUmount(t, volumeId, image, targetFoo,"",false)
	testMountAndUmount(t, volumeId, image, targetBar,"",true)

	m := NewMounter(containerdSock)
	if err := m.Unmount(context.TODO(), volumeId, targetFoo); err != nil {
		t.Error(err)
		t.Fail()
	}
}

func TestMountAsEphemeralVolume(t *testing.T) {
	image := "warmmetal/csi-image-test:simple-fs"
	volumeId := "csi-f608d82983355e90fbed86a57381947c2e8b164bfc584297f1c7a2b69fa1b295"
	target := "/tmp/image-mount-point/simple-fs"

	testMountAndUmount(t, volumeId, image, target, "",true)
}

func TestMountPrivateImages(t *testing.T) {
	image := "kitt0hsu/private-image:simple-fs"
	volumeId := "csi-f608d82983355e90fbed86a57381947c2e8b164bfc584297f1c7a2b69fa1b295"
	target := "/tmp/image-mount-point/simple-fs"

	testMountAndUmount(t, volumeId, image, target, "unit-test",true)
}
