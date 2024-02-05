package remoteimageasync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/warm-metal/container-image-csi-driver/pkg/remoteimage"
)

// sessionChanDepth : 100 - must give lots of buffer to ensure no deadlock or dropped requests
// completedChanDepth : 20 - must give some buffer to ensure no deadlock
func StartAsyncPuller(ctx context.Context, sessionChanDepth, completedChanDepth int) AsyncPuller {
	sessionChan := make(chan PullSession, sessionChanDepth)
	completedChan := make(chan string, completedChanDepth)
	async := getSynchronizer(
		ctx,
		sessionChan,
		completedChan,
	)
	async.RunCompletionsChecker()
	RunPullerLoop(ctx, sessionChan, completedChan)
	return async
}

// channels are exposed for testing
func getSynchronizer(
	ctx context.Context,
	sessionChan chan PullSession,
	completedChan chan string,
) synchronizer {
	if cap(sessionChan) < 50 {
		panic("session channel must have capacity to buffer events, minimum of 50 is required")
	}
	if cap(completedChan) < 5 {
		panic("completion channel must have capacity to buffer events, minimum of 5 is required")
	}
	return synchronizer{
		sessionMap:      make(map[string]PullSession),
		mutex:           &sync.Mutex{},
		sessions:        sessionChan,
		completedEvents: completedChan,
		ctx:             ctx,
	}
}

func (s synchronizer) StartPull(image string, puller remoteimage.Puller, asyncPullTimeout time.Duration) (PullSession, error) {
	fmt.Printf("start pull: asked to pull image %s\n", image)
	s.mutex.Lock() // lock mutex
	defer s.mutex.Unlock()
	ses, ok := s.sessionMap[image] // try get session
	if !ok {                       // if no session, create session
		ses = PullSession{
			image:      image,
			puller:     puller,
			timeout:    asyncPullTimeout,
			done:       make(chan interface{}),
			isComplete: false,
			isTimedOut: false,
			err:        nil,
		}
		select {
		case s.sessions <- ses: // start session, check for deadlock
			fmt.Printf("start pull: new session created for %s with timeout %v\n", ses.image, ses.timeout)
		default: // catch deadlock or throttling (they will look the same)
			ses.err = fmt.Errorf("start pull: cannot pull %s at this time, throttling or deadlock condition exists, retry if throttling", ses.image)
			ses.done <- true
			return ses, ses.err
		}
		s.sessionMap[image] = ses // add session to map
	} else {
		fmt.Printf("start pull: found open session for %s\n", ses.image)
	}
	// return session and unlock
	return ses, nil
}

func (s synchronizer) WaitForPull(session PullSession, callerTimeout context.Context) error {
	fmt.Printf("wait for pull: starting to wait for image %s\n", session.image)
	defer fmt.Printf("wait for pull: exiting wait for image %s\n", session.image)
	select {
	case <-session.done: // success or error (including session timeout)
		fmt.Printf("wait for pull: pull completed for %s, isError: %t\n",
			session.image, session.err != nil)
		return session.err
	case <-callerTimeout.Done():
		return fmt.Errorf("wait for pull: this wait for image %s has timed out due to caller context cancellation, pull likely continues in the background",
			session.image)
	case <-s.ctx.Done(): //TODO: might wait for puller to do this instead
		return fmt.Errorf("wait for pull: synchronizer is shutting down") // must return error since not success
	}
}

func (s synchronizer) RunCompletionsChecker() {
	go func() {
		shutdown := func() {
			s.mutex.Lock()
			for image := range s.sessionMap {
				delete(s.sessionMap, image) // no-op if already deleted due to race
			}
			s.mutex.Unlock()
		}
		defer shutdown()

		for {
			select {
			case <-s.ctx.Done(): // shut down loop
				close(s.sessions) // the writer is supposed to close channels
				// no need to process any future completed events, will panic if we close session channel again anyway
				return // deferred shutdown will do the work
			case image, ok := <-s.completedEvents: // remove session (no longer active)
				if ok {
					s.mutex.Lock()
					delete(s.sessionMap, image) // no-op if already deleted due to race
					s.mutex.Unlock()
				} else {
					return // deferred shutdown will do the work
				}
			}
		}
	}()
}
