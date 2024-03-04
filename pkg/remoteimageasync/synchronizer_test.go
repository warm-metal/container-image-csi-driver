package remoteimageasync

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// this demonstrates that session errors are consistent across go routines
func TestAsyncPullErrorReturn(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 5*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100)

	err := pullImage(puller, "nginx:exists", 1, 5, 5)
	assert.Nil(t, err, "no error should be returned for successful pull")

	err = pullImage(puller, nonExistentImage, 1, 5, 5)
	assert.NotNil(t, err, "error should be returned for non-existent image")

	<-ctx.Done()
}

// demonstrates pullerMock is functioning properly
// verifies parallelism
// checks correct image pulls completed withing set time (5s)
func TestPullDuration(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 5*time.Second) // shut down execution
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100)
	var ops atomic.Int32

	durations := []int{1, 2, 3, 4, 6, 7, 8}

	for _, dur := range durations {
		go func(dur int) {
			err := pullImage(puller, fmt.Sprintf("nginx:%v", dur), dur, 10, 10)
			if err == nil { // rejects pull results that are returned due to shutting down puller loop, otherwise a race condition
				ops.Add(1)
			}
		}(dur)
	}

	<-ctx.Done() // stop waiting when context times out (shut down)
	assert.Equal(t, 4, int(ops.Load()), "only 4 of %v should complete", len(durations))
}

// checks for call serialization
func TestParallelPull(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 3*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100)
	var ops atomic.Int32

	imgs := []int{2, 2, 2, 2, 2, 2, 2}

	for _, i := range imgs {
		go func(i int) {
			err := pullImage(puller, fmt.Sprintf("nginx:%v", i), i, 10, 10)
			if err == nil {
				ops.Add(1)
			}
		}(i)
	}

	<-ctx.Done()
	assert.Equal(t, len(imgs), int(ops.Load()), "all %v should complete", len(imgs))
}

// tests timeouts and eventual success of long image pull
func TestSerialResumedSessions(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 6*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100)
	var success atomic.Int32
	var notSuccess atomic.Int32

	// 3 states exist for each pull: running, success, error
	pull := func(image string, pullSec, asyncTimeoutSec, callerTimeoutSec int) {
		err := pullImage(puller, image, pullSec, asyncTimeoutSec, callerTimeoutSec)
		if err == nil {
			success.Add(1)
		} else {
			notSuccess.Add(1)
		}
	}

	// these are serial, not parallel. simulates kubelet retrying call to NodePublishVolume().
	pull("nginx:A", 5, 6, 1) // caller times out after 1s but pull continues asynchronously
	pull("nginx:A", 5, 6, 1) // continues session but caller times out after 1s
	pull("nginx:A", 5, 6, 1) // continues session but caller times out after 1s
	pull("nginx:A", 5, 6, 1) // continues session but caller times out after 1s
	assert.Equal(t, 0, int(success.Load()), "none should have finished yet")
	assert.Equal(t, 4, int(notSuccess.Load()), "all should have errored so far") // needed because 3 states exist

	pull("nginx:A", 5, 6, 2) // succeed after 1s because 5s (pull time) has elapsed since session started
	assert.Equal(t, 1, int(success.Load()), "1 should have finished")
	assert.Equal(t, 4, int(notSuccess.Load()), "no new errors after previous")

	<-ctx.Done()
}

// simulates multiple pods trying to mount same image
// this would result in parallel NodePublishVolume() calls to pull and mount same image
// demonstrates timeout and async pull continuation under that scenario
func TestParallelResumedSessions(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 6*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100)
	var success atomic.Int32
	var notSuccess atomic.Int32

	pull := func(image string, pullSec, asyncTimeoutSec, callerTimeoutSec int) {
		err := pullImage(puller, image, pullSec, asyncTimeoutSec, callerTimeoutSec)
		if err == nil {
			success.Add(1)
		} else {
			notSuccess.Add(1)
		}
	}

	pull("nginx:A", 5, 6, 1) // caller times out after 1s, pull continues async
	assert.Equal(t, 0, int(success.Load()), "none should have finished yet")
	assert.Equal(t, 1, int(notSuccess.Load()), "all should have errored so far") // caller timeout error returned

	time.Sleep(1 * time.Second)
	// time is now 2 sec into 5 sec pull

	// make parallel pull requests which would time out if not resuming session
	go func() { pull("nginx:A", 5, 6, 4) }() // caller allows 4s but completes in 3s
	go func() { pull("nginx:A", 5, 6, 4) }() // caller allows 4s but completes in 3s
	go func() { pull("nginx:A", 5, 6, 4) }() // caller allows 4s but completes in 3s
	assert.Equal(t, 0, int(success.Load()), "none should have finished yet")
	assert.Equal(t, 1, int(notSuccess.Load()), "all should have errored so far") // 1 timed out, 3 in-flight blocked waiting

	time.Sleep(3100 * time.Millisecond) // all should have succeeded 100ms ago

	assert.Equal(t, 3, int(success.Load()), "3 resumed calls should have finished")
	assert.Equal(t, 1, int(notSuccess.Load()), "no new errors")

	<-ctx.Done()
}

// pullDurationSec: typically 5-60 seconds, containerd behavior (time actually required to pull image)
// asyncPullTimeoutSec: ~10m, the new logic allows async continuation of a pull (if enabled)
// callerTimeoutSec: kubelet hard coded to 2m
func pullImage(puller AsyncPuller, image string, pullDurationSec, asyncPullTimeoutSec, callerTimeoutSec int) error {
	return pullImageRand(puller, image, pullDurationSec, pullDurationSec, asyncPullTimeoutSec, callerTimeoutSec)
}

func pullImageRand(puller AsyncPuller, image string, pullDurationSecLow, pullDurationSecHigh, asyncPullTimeoutSec, callerTimeoutSec int) error {
	pull := getPullerMockRand(image, pullDurationSecLow*1000, pullDurationSecHigh*1000)
	session, err := puller.StartPull(image, pull, time.Duration(asyncPullTimeoutSec)*time.Second)
	if err != nil {
		return err
	}
	ctx, dontCare := context.WithTimeout(context.TODO(), time.Duration(callerTimeoutSec)*time.Second)
	defer dontCare()
	return puller.WaitForPull(session, ctx)
}

type pullerMock struct {
	image          string
	msDurationLow  int
	msDurationHigh int
	size           int // negative size indicates error should be returned
}

func getPullerMock(image string, ms_duration int) pullerMock {
	return getPullerMockRand(image, ms_duration, ms_duration)
}

func getPullerMockRand(image string, ms_low, ms_high int) pullerMock {
	return pullerMock{
		image:          image,
		msDurationLow:  ms_low,
		msDurationHigh: ms_high,
		size:           0,
	}
}

// negative size indicates error should be returned
func (p *pullerMock) SetSize(size int) {
	p.size = size
}

func (p pullerMock) Pull(ctx context.Context) (err error) {
	dur := time.Duration(p.msDurationLow) * time.Millisecond
	if p.msDurationLow != p.msDurationHigh {
		rand.Seed(time.Now().UnixNano()) // without seed, same sequence always returned
		dur = time.Duration(p.msDurationLow+rand.Intn(p.msDurationHigh-p.msDurationLow)) * time.Millisecond
	}

	fmt.Printf("pullerMock: starting to pull image %s\n", p.image)
	if p.image == nonExistentImage {
		err = fmt.Errorf("pullerMock: non-existent image specified, returning this error\n")
		fmt.Println(err.Error())
		return err
	}
	select {
	case <-time.After(dur):
		fmt.Printf("pullerMock: finshed pulling image %s\n", p.image)
		return nil
	case <-ctx.Done():
		fmt.Printf("pullerMock: context cancelled\n")
		return nil
	}
}

func (p pullerMock) ImageWithTag() string {
	return p.image
}

func (p pullerMock) ImageWithoutTag() string {
	panic("Not implemented")
}

func (p pullerMock) ImageSize(ctx context.Context) (int, error) {
	if p.size < 0 {
		return 0, fmt.Errorf("error occurred when checking image size")
	}
	return p.size, nil
}

// this image is known to not exist and is used by integration tests for that purpose
const nonExistentImage = "docker.io/warmmetal/container-image-csi-driver-test:simple-fs-doesnt-exist"
