package main

import (
    "context"
    "flag"
    "github.com/golang/glog"
    "os"
    "path/filepath"
    "testing"
)

func TestMountAndUmount(t *testing.T) {
    flag.Set("logtostderr", "true")
    defer glog.Flush()
    id := "test-volume-id"
    image := "warmmetal/csi-image-test:simple-fs"
    target := "/tmp/image-mount-point/simple-fs"
    *defaultContainerdNamespace = "buildkit"
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
}
