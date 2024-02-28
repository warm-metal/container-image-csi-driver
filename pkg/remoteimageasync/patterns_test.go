package remoteimageasync

import (
	"fmt"
	"strings"
	"testing"

	"github.com/containerd/containerd/reference/docker"
	"github.com/stretchr/testify/assert"
	"github.com/warm-metal/container-image-csi-driver/pkg/remoteimage"
)

// demonstrates session channel structure's pass-by-reference is appropriate
func TestChannelStructContent(t *testing.T) {
	input1 := make(chan PullSession, 1)
	val1 := PullSession{
		err: nil,
	}
	assert.Nil(t, val1.err)
	input1 <- val1
	tmp1 := <-input1
	tmp1.err = fmt.Errorf("test1")
	assert.NotNil(t, tmp1.err)
	assert.Nil(t, val1.err, "pass by value does not update value")

	input2 := make(chan *PullSession, 1)
	val2 := PullSession{
		err: nil,
	}
	assert.Nil(t, val2.err)
	input2 <- &val2
	tmp2 := <-input2
	tmp2.err = fmt.Errorf("test2")
	assert.NotNil(t, tmp2.err)
	assert.NotNil(t, val2.err, "pass by reference does update value")
}

// demonstrates logic used in remoteimageasync.StartPull()
func TestChannelClose(t *testing.T) {
	input1 := make(chan interface{}, 5)
	result := 0

	select {
	case input1 <- 0:
		result = 1
	default:
		result = -1
	}
	assert.Equal(t, 1, result, "write should succeed")

	assert.Panics(t, func() {
		close(input1)
		select {
		case input1 <- 0:
			result = 2
		default:
			result = -2
		}
	}, "write should panic")

	var err error = nil
	assert.NotPanics(t, func() {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("recovered from %v", rec)
			}
		}()
		select {
		case input1 <- 0:
			result = 3
		default:
			result = -3
		}
	}, "write should not panic")
	assert.NotNil(t, err, "error should have been returned")
	assert.Contains(t, err.Error(), "closed", "error should indicate channel closed")
}

func TestNamedImageExtraction(t *testing.T) {
	parsed, err := docker.ParseDockerRef(nonExistentImage)
	assert.Nil(t, err, "parsing image name should succeed")
	puller := remoteimage.NewPuller(nil, parsed, nil)
	assert.Equal(t, nonExistentImage, puller.ImageWithTag(), "extracted value should match exactly %v", puller)
	repo := strings.Split(nonExistentImage, ":")[0]
	assert.Equal(t, repo, puller.ImageWithoutTag(), "extracted value should match exactly %v", puller)
}
