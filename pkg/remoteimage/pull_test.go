package remoteimage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/warm-metal/csi-driver-image/pkg/cri"
	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Check test/integration/node-server/README.md for how to run this test correctly
func TestPull(t *testing.T) {
	testImage := "docker.io/library/redis:latest"
	socketAddr := "unix:///run/containerd/containerd.sock"
	// addr, err := url.Parse(socketAddr)
	// assert.NoError(t, err)
	criClient, err := cri.NewRemoteImageService(socketAddr, time.Minute)
	assert.NoError(t, err)
	assert.NotNil(t, criClient)

	r, err := criClient.PullImage(context.Background(), &v1.PullImageRequest{
		Image: &v1.ImageSpec{
			Image: testImage,
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, r)
}
