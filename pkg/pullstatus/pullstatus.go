package pullstatus

import (
	"sync"

	"github.com/containerd/containerd/reference/docker"
)

// ImagePullStatus represents pull status of an image
type ImagePullStatus int

// https://stackoverflow.com/questions/14426366/what-is-an-idiomatic-way-of-representing-enums-in-go
const (
	// StatusNotFound means there has been no attempt to pull the image
	StatusNotFound ImagePullStatus = -1
	// StillPulling means the image is still being pulled
	StillPulling ImagePullStatus = iota
	// Pulled means the image has been pulled
	Pulled
	// Errored means there was an error during image pull
	Errored
)

// ImagePullStatusRecorder records the status of image pulls
type ImagePullStatusRecorder struct {
	status map[docker.Named]ImagePullStatus
	mutex  sync.Mutex
}

var i ImagePullStatusRecorder

func init() {
	i = ImagePullStatusRecorder{
		status: make(map[docker.Named]ImagePullStatus),
		mutex:  sync.Mutex{},
	}
}

// Update updates the pull status of an image
func Update(imageRef docker.Named, status ImagePullStatus) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.status[imageRef] = status
}

// Delete deletes the pull status of an image
func Delete(imageRef docker.Named) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	delete(i.status, imageRef)
}

// Get gets the pull status of an image
func Get(imageRef docker.Named) ImagePullStatus {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	if _, ok := i.status[imageRef]; !ok {
		return StatusNotFound
	}

	return i.status[imageRef]
}
