package pullstatus

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdate(t *testing.T) {
	i := &StatusRecorder{
		status: make(map[string]Status),
		mutex:  sync.Mutex{},
	}

	i.status["key1"] = StillProcessing

	// update existing key with a new value
	i.Update("key1", Processed)
	assert.Equal(t, Processed, i.status["key1"])
	assert.Equal(t, Processed, i.Get("key1"))

	// add new key and value
	PullStatus.Update("key2", StillProcessing)
	assert.Equal(t, StillProcessing, i.status["key2"])
	assert.Equal(t, StillProcessing, i.Get("key2"))

}

func TestDelete(t *testing.T) {
	i := &StatusRecorder{
		status: make(map[string]Status),
		mutex:  sync.Mutex{},
	}
	i.status["key1"] = StillProcessing

	// delete a key
	i.Delete("key1")
	assert.Equal(t, 0, int(i.status["key1"]))
	assert.Equal(t, StatusNotFound, i.Get("key1"))
}

func TestGet(t *testing.T) {
	i := &StatusRecorder{
		status: make(map[string]Status),
		mutex:  sync.Mutex{},
	}
	i.status["key1"] = StillProcessing

	assert.Equal(t, i.status["key1"], i.Get("key1"))
}

func TestKey(t *testing.T) {
	assert.Equal(t, "foo-bar", CompositeKey("foo", "bar"))
}
