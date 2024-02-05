package remoteimageasync

import (
	"context"
	"fmt"
	"time"
)

func RunPullerLoop(
	ctx context.Context,
	sessionChan chan PullSession,
	completedChan chan string,
) {
	go func() {
		for {
			select {
			case <-ctx.Done(): // shut down loop
				// close(completedChan) // the writer is supposed to close channels, but this is messy due to async ops... leaving it open
				return
			case ses, ok := <-sessionChan:
				if !ok { // sessionChan closed, shut down loop
					return
				}
				go func() {
					fmt.Printf("puller: asked to pull image %s with timeout %v\n",
						ses.image, ses.timeout)
					ctxCombined, cancelDontCare := context.WithTimeout(ctx, ses.timeout) // combine timeout and shut down signal into one
					defer cancelDontCare()                                               // IF we exit, this no longer matters. calling to satisfy linter.
					//NOTE: the logic for "mustPull" is not needed so long as we are not throttling.
					//      if we DO implement throttling, then additional logic might be required.
					// mustPull := !cri.hasImage(ses.image)
					pullStart := time.Now()
					// if mustPull {
					// 	fmt.Printf("puller: image not found, pulling %s\n", ses.image)
					// 	cri.pullImage(ses.image, ctx2)
					// }
					pullErr := ses.puller.Pull(ctxCombined) //NOTE: relying existing tests or history to veirfy behavior, asyncPull just wraps it
					// update fields
					select {
					case <-ctx.Done(): // shutting down
						ses.isComplete = false
						ses.isTimedOut = false
						ses.err = fmt.Errorf("puller: shutting down")
						fmt.Println(ses.err.Error())
					case <-ctxCombined.Done():
						ses.isComplete = false
						ses.isTimedOut = true
						ses.err = fmt.Errorf("puller: async pull exceeded timeout of %v for image %s", ses.timeout, ses.image)
						fmt.Println(ses.err.Error())
					default:
						ses.isComplete = true
						ses.isTimedOut = false
						ses.err = pullErr
						// if mustPull {
						fmt.Printf("puller: pull completed in %v for image %s\n", time.Since(pullStart), ses.image)
						// } else {
						// 	fmt.Printf("puller: image already present for %s\n", ses.image)
						// }
					}
					close(ses.done) // signal done
					//NOTE: writing to completedChan could error if already closed above... that's ok because everything would be shutting down.
					//NOTE: also, it could block until the completion processor catches up, which is ok.
					completedChan <- ses.image // this must be open when this completes or we'd have to recover from a panic
				}()
			}
		}

	}()
}
