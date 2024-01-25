package pullstatus

import (
	"fmt"
	"sync"
)

// Status represents the status
type Status int

// https://stackoverflow.com/questions/14426366/what-is-an-idiomatic-way-of-representing-enums-in-go
const (
	// Errored means there was an error during processing
	Errored Status = iota - 1
	// StatusNotFound means there has been no attempt to process
	StatusNotFound
	// StillProcessing means the processing is happening
	StillProcessing
	// Processed means the operation has been fulfilled
	Processed
)

func (s Status) String() string {
	switch int(s) {
	case -1:
		return "Errored"
	case 0:
		return "StatusNotFound"
	case 1:
		return "StillProcessing"
	case 2:
		return "Processed"
	default:
		panic(fmt.Sprintf("unidentified status: %v", int(s)))
	}

}

// StatusRecorder records the status
type StatusRecorder struct {
	status map[string]Status
	mutex  sync.Mutex
}

var PullStatus *StatusRecorder
var MountStatus *StatusRecorder

func init() {
	PullStatus = &StatusRecorder{
		status: make(map[string]Status),
		mutex:  sync.Mutex{},
	}
	MountStatus = &StatusRecorder{
		status: make(map[string]Status),
		mutex:  sync.Mutex{},
	}
}

// Update updates the status
func (i *StatusRecorder) Update(key string, status Status) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.status[key] = status
}

// Delete deletes the status
func (i *StatusRecorder) Delete(key string) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	delete(i.status, key)
}

// Get gets the status
func (i *StatusRecorder) Get(key string) Status {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	if _, ok := i.status[key]; !ok {
		return StatusNotFound
	}

	return i.status[key]
}

// CompositeKey creates a composite key out of two keys
// (for cases when one key is not unique enough)
func CompositeKey(key1 string, key2 string) string {
	return fmt.Sprintf("%s-%s", key1, key2)
}
