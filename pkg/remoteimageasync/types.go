package remoteimageasync

import (
	"context"
	"sync"
	"time"

	"github.com/warm-metal/container-image-csi-driver/pkg/remoteimage"
)

const prefix = "remoteimageasync"

type PullSession struct {
	puller     remoteimage.Puller
	timeout    time.Duration    // this is the session timeout, not the caller timeout
	done       chan interface{} // chan will block until result
	isTimedOut bool
	err        error
}

func (p PullSession) ImageWithTag() string {
	return p.puller.ImageWithTag()
}

type synchronizer struct {
	sessionMap map[string]*PullSession // all interactions must be mutex'd
	mutex      *sync.Mutex             // this exclusively protects the sessionMap
	sessions   chan *PullSession       // pull activity occurs in puller Go routine when using async mode
	ctx        context.Context         // top level application context
}

// allows mocking/dependency injection
type AsyncPuller interface {
	// returns session that is ready to wait on, or error
	StartPull(image string, puller remoteimage.Puller, asyncPullTimeout time.Duration) (*PullSession, error)
	// waits for session to time out or succeed
	WaitForPull(session *PullSession, callerTimeout context.Context) error
}
