package mountstatus

import (
	"sync"
)

// ImagePullStatus represents mount status of an image
type ImageMountStatus int

// https://stackoverflow.com/questions/14426366/what-is-an-idiomatic-way-of-representing-enums-in-go
const (
	// StatusNotFound means there has been no attempt to mount the image
	StatusNotFound ImageMountStatus = -1
	// StillMounting means the image is still being mounted as a volume
	StillMounting ImageMountStatus = iota
	// Mounted means the image has been mounted as a volume
	Mounted
	// Errored means there was an error during image mount
	Errored
)

// ImageMountStatusRecorder records the status of image mounts
type ImageMountStatusRecorder struct {
	status map[string]ImageMountStatus
	mutex  sync.Mutex
}

var i ImageMountStatusRecorder

func init() {
	i = ImageMountStatusRecorder{
		status: make(map[string]ImageMountStatus),
		mutex:  sync.Mutex{},
	}
}

// Update updates the mount status of an image
func Update(volumeId string, status ImageMountStatus) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.status[volumeId] = status
}

// Delete deletes the mount status of an image
func Delete(volumeId string) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	delete(i.status, volumeId)
}

// Get gets the mount status of an image
func Get(volumeId string) ImageMountStatus {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	if _, ok := i.status[volumeId]; !ok {
		return StatusNotFound
	}

	return i.status[volumeId]
}
