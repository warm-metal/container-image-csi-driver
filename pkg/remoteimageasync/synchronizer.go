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
	sessionChan := make(chan *PullSession, sessionChanDepth)
	completedChan := make(chan string, completedChanDepth)
	async := getSynchronizer(
		ctx,
		sessionChan,
		completedChan,
	)
	async.RunCompletionsLoop()
	RunPullerLoop(ctx, sessionChan, completedChan)
	klog.Infof("%s.StartAsyncPuller(): async puller is operational", prefix)
	return async
}

// channels are exposed for testing
func getSynchronizer(
	ctx context.Context,
	sessionChan chan *PullSession,
	completedChan chan string,
) synchronizer {
	if cap(sessionChan) < 50 {
		klog.Fatalf("%s.getSynchronizer(): session channel must have capacity to buffer events, minimum of 50 is required", prefix)
	}
	if cap(completedChan) < 5 {
		klog.Fatalf("%s.getSynchronizer(): completion channel must have capacity to buffer events, minimum of 5 is required", prefix)
	}
	return synchronizer{
		sessionMap:      make(map[string]*PullSession),
		mutex:           &sync.Mutex{},
		sessions:        sessionChan,
		completedEvents: completedChan,
		ctx:             ctx,
	}
}

func (s synchronizer) StartPull(image string, puller remoteimage.Puller, asyncPullTimeout time.Duration) (ses *PullSession, err error) {
	klog.V(2).Infof("%s.StartPull(): start pull: asked to pull image %s", prefix, image)
	s.mutex.Lock() // lock mutex, no blocking sends/receives inside mutex
	defer s.mutex.Unlock()
	ses, ok := s.sessionMap[image] // try get session
	if !ok {                       // if no session, create session
		ses = &PullSession{
			image:      image,
			puller:     puller,
			timeout:    asyncPullTimeout,
			done:       make(chan interface{}),
			isComplete: false,
			isTimedOut: false,
			err:        nil,
		}

		defer func() {
			if rec := recover(); rec != nil { // handle session write panic due to closed sessionChan
				// override named return values
				ses = nil
				err = fmt.Errorf("%s.StartPull(): cannot create pull session for %s at this time, reason: %v", prefix, ses.image, rec)
				klog.V(2).Info(err.Error())
			}
		}()
		select {
		case s.sessions <- ses: // start session, check for deadlock... possibility of panic but only during app shutdown where Puller has already ceased to operate, handle with defer/recover
			klog.V(2).Infof("%s.StartPull(): new session created for %s with timeout %v", prefix, ses.image, ses.timeout)
			s.sessionMap[image] = ses // add session to map to allow continuation... only do this because was passed to puller via sessions channel
			return ses, nil
		default: // catch deadlock or throttling (they may look the same)
			err := fmt.Errorf("%s.StartPull(): cannot create pull session for %s at this time, throttling or deadlock condition exists, retry if throttling", prefix, ses.image)
			klog.V(2).Info(err.Error())
			return nil, err
		}
	} else {
		klog.V(2).Infof("%s.StartPull(): found open session for %s", prefix, ses.image)
		// return session and unlock
		return ses, nil
	}
}

func (s synchronizer) WaitForPull(session *PullSession, callerTimeout context.Context) error {
	klog.V(2).Infof("%s.WaitForPull(): starting to wait for image %s", prefix, session.image)
	defer klog.V(2).Infof("%s.WaitForPull(): exiting wait for image %s", prefix, session.image)
	select {
	case <-session.done: // success or error (including session timeout and shutting down)
		klog.V(2).Infof("%s.WaitForPull(): session completed with success or error for image %s, error=%v", prefix, session.image, session.err)
		return session.err
	case <-callerTimeout.Done(): // caller timeout
		err := fmt.Errorf("%s.WaitForPull(): this wait for image %s has timed out due to caller context cancellation, pull likely continues in the background",
			prefix, session.image)
		klog.V(2).Info(err.Error())
		return err
	}
}

// NOTE: all sessions that are successfully submitted to sessionsChan must be submitted to completedEvents
func (s synchronizer) RunCompletionsLoop() {
	go func() {
		klog.V(2).Infof("%s.RunCompletionsLoop(): starting", prefix)
		for image := range s.completedEvents { // remove session (no longer active)
			s.mutex.Lock()
			klog.V(2).Infof("%s.RunCompletionsLoop(): clearing session for %s", prefix, image)
			delete(s.sessionMap, image) // no-op if already deleted
			s.mutex.Unlock()
		}
		klog.V(2).Infof("%s.RunCompletionsLoop(): exiting loop", prefix)
	}()
}
