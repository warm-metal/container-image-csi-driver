package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/distribution/reference"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"google.golang.org/grpc"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type MockImageServiceClient struct {
	PulledImages  map[string]bool
	ImagePullTime time.Duration
}

type MockMounter struct {
	ImageSvcClient MockImageServiceClient
	Mounted        map[string]bool
}

const hundredMB = 104857600

func (m *MockMounter) Mount(
	ctx context.Context, volumeId string, target backend.MountTarget, image reference.Named, ro bool) (err error) {
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
func (m *MockMounter) ImageExists(ctx context.Context, image reference.Named) bool {
	return m.ImageSvcClient.PulledImages[image.Name()]
}

func (c *MockImageServiceClient) ListImages(ctx context.Context, in *criapi.ListImagesRequest, opts ...grpc.CallOption) (*criapi.ListImagesResponse, error) {
	resp := new(criapi.ListImagesResponse)
	resp.Images = []*criapi.Image{}

	for k := range c.PulledImages {
		resp.Images = append(resp.Images, &criapi.Image{
			Id: k,
			// 100MB
			Size: hundredMB,
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
		Id:   in.Image.Image,
		Size: hundredMB,
		Spec: &criapi.ImageSpec{
			Image: in.Image.Image,
		},
	}
	return resp, nil
}

func (c *MockImageServiceClient) PullImage(ctx context.Context, in *criapi.PullImageRequest, opts ...grpc.CallOption) (*criapi.PullImageResponse, error) {
	resp := new(criapi.PullImageResponse)
	resp.ImageRef = in.Image.Image
	time.Sleep(c.ImagePullTime)

	return resp, nil
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
				Value: hundredMB,
			},
			InodesUsed: &criapi.UInt64Value{
				// random value
				Value: 10,
			},
		})
	}

	return resp, nil
}
