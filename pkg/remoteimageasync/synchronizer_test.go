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

func TestPullDuration(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 5*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100, 20)
	var ops atomic.Int32

	imgs := []int{1, 2, 3, 4, 6, 7, 8}

	for _, i := range imgs {
		go func(i int) {
			err := pullImage(puller, fmt.Sprintf("nginx:%v", i), i, 10, 10)
			if err == nil {
				ops.Add(1)
			}
		}(i)
	}

	<-ctx.Done()
	assert.Equal(t, 4, int(ops.Load()), "only 4 of %v should complete", len(imgs))
}

func TestParallelPull(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 3*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100, 20)
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

func TestSerialResumedSessions(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 6*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100, 20)
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

	pull("nginx:A", 5, 6, 1)
	pull("nginx:A", 5, 6, 1)
	pull("nginx:A", 5, 6, 1)
	pull("nginx:A", 5, 6, 1)
	assert.Equal(t, 0, int(success.Load()), "none should have finished yet")
	assert.Equal(t, 4, int(notSuccess.Load()), "all should have errored so far")

	pull("nginx:A", 5, 6, 1)
	assert.Equal(t, 1, int(success.Load()), "1 should have finished")
	assert.Equal(t, 4, int(notSuccess.Load()), "no new errors after previous")

	<-ctx.Done()
}

func TestParallelResumedSessions(t *testing.T) {
	ctx, dontCare := context.WithTimeout(context.TODO(), 6*time.Second)
	defer dontCare()
	puller := StartAsyncPuller(ctx, 100, 20)
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

	pull("nginx:A", 5, 6, 1)
	assert.Equal(t, 0, int(success.Load()), "none should have finished yet")
	assert.Equal(t, 1, int(notSuccess.Load()), "all should have errored so far")

	time.Sleep(1 * time.Second)
	// time is now 2 sec into 5 sec pull

	// make parallel pull requests which would time out if not resuming session
	go func() { pull("nginx:A", 5, 6, 4) }()
	go func() { pull("nginx:A", 5, 6, 4) }()
	go func() { pull("nginx:A", 5, 6, 4) }()
	assert.Equal(t, 0, int(success.Load()), "none should have finished yet")
	assert.Equal(t, 1, int(notSuccess.Load()), "all should have errored so far")

	time.Sleep(3100 * time.Millisecond) // all should have succeeded 100ms ago

	assert.Equal(t, 3, int(success.Load()), "3 resumed calls should have finished")
	assert.Equal(t, 1, int(notSuccess.Load()), "no new errors")

	<-ctx.Done()
}

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
}

func getPullerMock(image string, ms_duration int) pullerMock {
	return pullerMock{
		image:          image,
		msDurationLow:  ms_duration,
		msDurationHigh: ms_duration,
	}
}

func getPullerMockRand(image string, ms_low, ms_high int) pullerMock {
	return pullerMock{
		image:          image,
		msDurationLow:  ms_low,
		msDurationHigh: ms_high,
	}
}

func (p pullerMock) Pull(ctx context.Context) (err error) {
	dur := time.Duration(p.msDurationLow) * time.Millisecond
	if p.msDurationLow != p.msDurationHigh {
		rand.Seed(time.Now().UnixNano()) // without seed, same sequence always returned
		dur = time.Duration(p.msDurationLow+rand.Intn(p.msDurationHigh-p.msDurationLow)) * time.Millisecond
	}

	fmt.Printf("pullerMock: starting to pull image %s\n", p.image)
	select {
	case <-time.After(dur):
		fmt.Printf("pullerMock: finshed pulling image %s\n", p.image)
		return
	case <-ctx.Done():
		fmt.Printf("pullerMock: context cancelled\n")
		return
	}
}

func (p pullerMock) ImageSize(ctx context.Context) int {
	return 0
}
