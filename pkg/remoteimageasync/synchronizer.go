package remoteimageasync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/warm-metal/container-image-csi-driver/pkg/remoteimage"
	"k8s.io/klog/v2"
)

// sessionChanDepth : 100 - must give lots of buffer to ensure no deadlock or dropped requests
// completedChanDepth : 20 - must give some buffer to ensure no deadlock
func StartAsyncPuller(ctx context.Context, sessionChanDepth, completedChanDepth int) AsyncPuller {
	klog.Infof("%s.StartAsyncPuller(): starting async puller", prefix)
	sessionChan := make(chan PullSession, sessionChanDepth)
	completedChan := make(chan string, completedChanDepth)
	async := getSynchronizer(
		ctx,
		sessionChan,
		completedChan,
	)
	async.RunCompletionsChecker()
	RunPullerLoop(ctx, sessionChan, completedChan)
	klog.Infof("%s.StartAsyncPuller(): async puller is operational", prefix)
	return async
}

// channels are exposed for testing
func getSynchronizer(
	ctx context.Context,
	sessionChan chan PullSession,
	completedChan chan string,
) synchronizer {
	if cap(sessionChan) < 50 {
		klog.Fatalf("%s.getSynchronizer(): session channel must have capacity to buffer events, minimum of 50 is required", prefix)
	}
	if cap(completedChan) < 5 {
		klog.Fatalf("%s.getSynchronizer(): completion channel must have capacity to buffer events, minimum of 5 is required", prefix)
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
	klog.V(2).Infof("%s.StartPull(): start pull: asked to pull image %s", prefix, image)
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
		case s.sessions <- ses: // start session, check for deadlock... possibility of panic but only during app shutdown where Puller has already ceased to operate
			klog.V(2).Infof("%s.StartPull(): new session created for %s with timeout %v", prefix, ses.image, ses.timeout)
		default: // catch deadlock or throttling (they will look the same)
			ses.err = fmt.Errorf("%s.StartPull(): cannot pull %s at this time, throttling or deadlock condition exists, retry if throttling", prefix, ses.image)
			klog.V(2).Info(ses.err.Error())
			ses.done <- true
			return ses, ses.err
		}
		s.sessionMap[image] = ses // add session to map
	} else {
		klog.V(2).Infof("%s.StartPull(): found open session for %s", prefix, ses.image)
	}
	// return session and unlock
	return ses, nil
}

func (s synchronizer) WaitForPull(session PullSession, callerTimeout context.Context) error {
	klog.V(2).Infof("%s.WaitForPull(): starting to wait for image %s", prefix, session.image)
	defer klog.V(2).Infof("%s.WaitForPull(): exiting wait for image %s", prefix, session.image)
	select {
	case <-session.done: // success or error (including session timeout)
		klog.V(2).Infof("%s.WaitForPull(): pull completed for %s, isError: %t, error: %v",
			prefix, session.image, session.err != nil, session.err)
		return session.err
	case <-callerTimeout.Done():
		err := fmt.Errorf("%s.WaitForPull(): this wait for image %s has timed out due to caller context cancellation, pull likely continues in the background",
			prefix, session.image)
		klog.V(2).Info(err.Error())
		return err
	case <-s.ctx.Done(): //TODO: might wait for puller to do this instead
		err := fmt.Errorf("%s.WaitForPull(): synchronizer is shutting down", prefix) // must return error since not success
		klog.V(2).Infof(err.Error())
		return err
	}
}

func (s synchronizer) RunCompletionsChecker() {
	go func() {
		klog.V(2).Infof("%s.RunCompletionsChecker(): starting", prefix)
		shutdown := func() {
			klog.V(2).Infof("%s.RunCompletionsChecker(): shutting down", prefix)
			s.mutex.Lock()
			for image := range s.sessionMap { // purge open sessions, continuation no longer allowed
				delete(s.sessionMap, image) // no-op if already deleted
			}
			close(s.sessions) // the writer is supposed to close channels
			// no need to process any future completed events
			s.mutex.Unlock()
		}
		defer shutdown()

		for {
			select {
			case <-s.ctx.Done(): // shut down loop
				return // deferred shutdown will do the work
			case image, ok := <-s.completedEvents: // remove session (no longer active)
				if ok {
					s.mutex.Lock()
					delete(s.sessionMap, image) // no-op if already deleted
					s.mutex.Unlock()
				} else { // channel closed, no further sessions can be created
					return // deferred shutdown will do the work
				}
			}
		}
	}()
}
