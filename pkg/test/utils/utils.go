package utils

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/containerd/containerd/reference/docker"
	"github.com/google/uuid"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	csicommon "github.com/warm-metal/csi-drivers/pkg/csi-common"
	"google.golang.org/grpc"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const hundredMiB = 104857600

type MockImageServiceClient struct {
	PulledImages  map[string]bool
	ImagePullTime time.Duration
}

type MockMounter struct {
	ImageSvcClient MockImageServiceClient
	Mounted        map[string]bool
}

type TestNonBlockingGRPCServer struct {
	csicommon.NonBlockingGRPCServer
	sockPath string
}

func BuildTestNonblockingGRPCServer() *TestNonBlockingGRPCServer {
	u, err := uuid.NewUUID()
	if err != nil {
		panic(err)
	}

	sockPath := fmt.Sprintf("/tmp/%s-csi.sock", u.String())

	// automatically deleted when the server is stopped
	if _, err := os.Create(sockPath); err != nil {
		panic(err)
	}

	return &TestNonBlockingGRPCServer{
		NonBlockingGRPCServer: csicommon.NewNonBlockingGRPCServer(),
		sockPath:              sockPath,
	}
}

func (t *TestNonBlockingGRPCServer) Start(ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) {
	t.NonBlockingGRPCServer.Start(fmt.Sprintf("unix://%s", t.sockPath), ids, cs, ns)
}

func (t *TestNonBlockingGRPCServer) SockPath() string {
	return t.sockPath
}

func (t *TestNonBlockingGRPCServer) Wait() {
	t.NonBlockingGRPCServer.Wait()
}

func (t *TestNonBlockingGRPCServer) Stop() {
	defer os.Remove(t.sockPath)
	t.NonBlockingGRPCServer.Stop()
}

func (t *TestNonBlockingGRPCServer) ForceStop() {
	defer os.Remove(t.sockPath)
	t.NonBlockingGRPCServer.ForceStop()
}

func (m *MockMounter) Mount(
	ctx context.Context, volumeId string, target backend.MountTarget, image docker.Named, ro bool) (err error) {
	m.Mounted[volumeId] = true
	return nil
}

// Unmount unmounts a specific image
func (m *MockMounter) Unmount(ctx context.Context, volumeId string, target backend.MountTarget) error {
	if m.Mounted[volumeId] {
		delete(m.Mounted, volumeId)
		return nil
	}
	return fmt.Errorf("image mount not found")
}

// ImageExists checks if the image already exists on the local machine
func (m *MockMounter) ImageExists(ctx context.Context, image docker.Named) bool {
	return m.ImageSvcClient.PulledImages[image.Name()]
}

func (c *MockImageServiceClient) ListImages(ctx context.Context, in *criapi.ListImagesRequest, opts ...grpc.CallOption) (*criapi.ListImagesResponse, error) {
	resp := new(criapi.ListImagesResponse)
	resp.Images = []*criapi.Image{}

	for k := range c.PulledImages {
		resp.Images = append(resp.Images, &criapi.Image{
			Id: k,
			// 100MB
			Size_: hundredMiB,
			Spec: &criapi.ImageSpec{
				Image: k,
			},
		})
	}

	return resp, nil
}

func (c *MockImageServiceClient) ImageStatus(ctx context.Context, in *criapi.ImageStatusRequest, opts ...grpc.CallOption) (*criapi.ImageStatusResponse, error) {
	resp := new(criapi.ImageStatusResponse)
	resp.Image = &criapi.Image{
		Id:    in.Image.Image,
		Size_: hundredMiB,
		Spec: &criapi.ImageSpec{
			Image: in.Image.Image,
		},
	}
	return resp, nil
}

func (c *MockImageServiceClient) PullImage(ctx context.Context, in *criapi.PullImageRequest, opts ...grpc.CallOption) (*criapi.PullImageResponse, error) {

	resp := new(criapi.PullImageResponse)
	resp.ImageRef = in.Image.Image

	var err error = nil

	if strings.HasSuffix(in.Image.Image, "INVALIDIMAGE") {
		resp = nil
		err = fmt.Errorf("mock puller: invalid image: %s", in.Image.Image)
	}

	time.Sleep(c.ImagePullTime)

	return resp, err
}

func (c *MockImageServiceClient) RemoveImage(ctx context.Context, in *criapi.RemoveImageRequest, opts ...grpc.CallOption) (*criapi.RemoveImageResponse, error) {
	resp := new(criapi.RemoveImageResponse)
	delete(c.PulledImages, in.Image.Image)
	return resp, nil
}

func (c *MockImageServiceClient) ImageFsInfo(ctx context.Context, in *criapi.ImageFsInfoRequest, opts ...grpc.CallOption) (*criapi.ImageFsInfoResponse, error) {
	resp := new(criapi.ImageFsInfoResponse)
	resp.ImageFilesystems = []*criapi.FilesystemUsage{}

	for _ = range c.PulledImages {
		resp.ImageFilesystems = append(resp.ImageFilesystems, &criapi.FilesystemUsage{
			Timestamp: time.Now().Unix(),
			FsId: &criapi.FilesystemIdentifier{
				Mountpoint: "target",
			},
			UsedBytes: &criapi.UInt64Value{
				Value: hundredMiB,
			},
			InodesUsed: &criapi.UInt64Value{
				// random value
				Value: 10,
			},
		})
	}

	return resp, nil
}
