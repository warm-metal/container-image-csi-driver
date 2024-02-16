package remoteimageasync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"k8s.io/klog/v2"
)

// sessionChan and completedChan both closed here
func RunPullerLoop(
	ctx context.Context,
	sessionChan chan *PullSession,
	completedChan chan string,
) {
	go func() {
		<-ctx.Done()
		close(sessionChan) // only close this once
	}()
	go func() {
		wg := sync.WaitGroup{}
		for {
			ses, ok := <-sessionChan // ctx not observed for shut down, this sleep breaks when sessionChan is closed
			if !ok {                 // sessionChan closed, shut down loop
				wg.Wait()            // wait for running goroutines (which observe cancellation)
				close(completedChan) // only close this once, after completions submitted (wg.Wait() returns)
				return
			}
			wg.Add(1) // increment before goroutine starts execution to avoid race condition
			go func() {
				defer wg.Done() // announce done upon completion, various ways to wake this goroutine are in place
				klog.V(2).Infof("%s.RunPullerLoop(): asked to pull image %s with timeout %v\n",
					prefix, ses.image, ses.timeout)
				ctxCombined, cancelDontCare := context.WithTimeout(ctx, ses.timeout) // combine timeout and shut down signal into one
				defer cancelDontCare()                                               // IF we exit, this no longer matters. calling to satisfy linter.
				pullStart := time.Now()
				pullErr := ses.puller.Pull(ctxCombined) //NOTE: relying existing tests or history to verify behavior, asyncPull just wraps it
				// update fields on session before declaring done
				select {
				case <-ctx.Done(): // shutting down
					ses.isComplete = false
					ses.isTimedOut = false
					ses.err = fmt.Errorf("%s.RunPullerLoop(): shutting down", prefix)
					klog.V(2).Infof(ses.err.Error())
					metrics.OperationErrorsCount.WithLabelValues("pull-async-shutdown").Inc()
				case <-ctxCombined.Done(): // timeout or shutdown
					ses.isComplete = false
					ses.isTimedOut = true
					ses.err = fmt.Errorf("%s.RunPullerLoop(): async pull exceeded timeout of %v for image %s", prefix, ses.timeout, ses.image)
					klog.V(2).Infof(ses.err.Error())
					metrics.OperationErrorsCount.WithLabelValues("pull-async-timeout").Inc()
				default: // completion: success or error
					ses.isComplete = true
					ses.isTimedOut = false
					ses.err = pullErr
					klog.V(2).Infof("%s.RunPullerLoop(): pull completed in %v for image %s with error=%v\n", prefix, time.Since(pullStart), ses.image, ses.err)
					if ses.err != nil {
						metrics.OperationErrorsCount.WithLabelValues("pull-async-error").Inc()
					}
				}
				close(ses.done) // signal done, all waiters should wake
				//NOTE: completedChan could block until the completion loop catches up, which is ok... it's work is trivial and gated only by sessionMap mutex
				completedChan <- ses.image // this will always be open on this send
			}()
		}
	}()
}
