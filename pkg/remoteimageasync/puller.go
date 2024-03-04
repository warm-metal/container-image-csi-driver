package remoteimageasync

import (
	"context"
	"fmt"
	"time"

	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"k8s.io/klog/v2"
)

// sessionChan and completedChan both closed here
func RunPullerLoop(
	ctx context.Context,
	sessionChan chan *PullSession,
	completedFunc func(*PullSession),
) {
	go func() {
		<-ctx.Done()
		close(sessionChan) // only close this once
	}()
	go func() {
		for {
			ses, ok := <-sessionChan // ctx not observed for shut down, this sleep breaks when sessionChan is closed
			if !ok {                 // sessionChan closed, shut down loop
				return
			}
			go func() {
				klog.V(2).Infof("%s.RunPullerLoop(): asked to pull image %s with timeout %v\n",
					prefix, ses.ImageWithTag(), ses.timeout)
				ctxAsyncPullTimeoutOrShutdown, cancelDontCare := context.WithTimeout(ctx, ses.timeout) // combine session timeout and shut down signal into one
				defer cancelDontCare()                                                                 // IF we exit, this no longer matters. calling to satisfy linter.
				pullStart := time.Now()
				pullErr := ses.puller.Pull(ctxAsyncPullTimeoutOrShutdown) // the waiting happens here, not in the select
				// update fields on session before declaring done
				select { // no waiting here, cases check for reason we exited puller.Pull()
				case <-ctx.Done(): // application shutting down
					ses.isTimedOut = false
					ses.err = fmt.Errorf("%s.RunPullerLoop(): shutting down", prefix)
					klog.V(2).Infof(ses.err.Error())
					metrics.OperationErrorsCount.WithLabelValues("pull-async-shutdown").Inc()
				case <-ctxAsyncPullTimeoutOrShutdown.Done(): // async pull timeout or shutdown
					ses.isTimedOut = true
					ses.err = fmt.Errorf("%s.RunPullerLoop(): async pull exceeded timeout of %v for image %s", prefix, ses.timeout, ses.ImageWithTag())
					klog.V(2).Infof(ses.err.Error())
					metrics.OperationErrorsCount.WithLabelValues("pull-async-timeout").Inc()
				default: // completion: success or error
					ses.isTimedOut = false
					ses.err = pullErr
					klog.V(2).Infof("%s.RunPullerLoop(): pull completed in %v for image %s with error=%v\n", prefix, time.Since(pullStart), ses.ImageWithTag(), ses.err)
					if ses.err != nil {
						metrics.OperationErrorsCount.WithLabelValues("pull-async-error").Inc()
					}
				}
				close(ses.done) // signal done, all waiters should wake
				completedFunc(ses)
			}()
		}
	}()
}
