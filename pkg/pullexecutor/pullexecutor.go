package pullexecutor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"github.com/pkg/errors"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/metrics"
	"github.com/warm-metal/csi-driver-image/pkg/pullstatus"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	"k8s.io/apimachinery/pkg/util/wait"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

const (
	pullPollTimeInterval = 100 * time.Millisecond
	pullPollTimeout      = 2 * time.Minute
	pullCtxTimeout       = 10 * time.Minute
)

// PullExecutorOptions are the options passed to the pull executor
type PullExecutorOptions struct {
	AsyncPull          bool
	ImageServiceClient cri.ImageServiceClient
	SecretStore        secret.Store
	Mounter            backend.Mounter
	MaxInflightPulls   int
}

// PullOptions are the options for a single pull request
type PullOptions struct {
	// Context here is only valid for synchronous mounts
	Context     context.Context
	NamedRef    docker.Named
	PullAlways  bool
	PullSecrets map[string]string
	Image       string
}

// PullExecutor executes the pulls
type PullExecutor struct {
	asyncPull      bool
	imageSvcClient cri.ImageServiceClient
	mutex          *sync.Mutex
	asyncErrs      map[string]error
	secretStore    secret.Store
	mounter        backend.Mounter
	tokens         chan struct{}
}

// NewPullExecutor initializes a new pull executor object
func NewPullExecutor(o *PullExecutorOptions) *PullExecutor {

	var tokens chan struct{}

	if o.MaxInflightPulls > 0 {
		tokens = make(chan struct{}, o.MaxInflightPulls)
	}

	return &PullExecutor{
		asyncPull:      o.AsyncPull,
		mutex:          &sync.Mutex{},
		imageSvcClient: o.ImageServiceClient,
		secretStore:    o.SecretStore,
		mounter:        o.Mounter,
		asyncErrs:      make(map[string]error),
		tokens:         tokens,
	}
}

// StartPulling starts pulling the image
func (m *PullExecutor) StartPulling(o *PullOptions) error {
	namedRef := o.NamedRef.String()

	keyring, err := m.secretStore.GetDockerKeyring(o.Context, o.PullSecrets)
	if err != nil {
		return errors.Errorf("unable to fetch keyring: %s", err)
	}

	if !m.asyncPull {
		puller := remoteimage.NewPuller(m.imageSvcClient, o.NamedRef, keyring)
		shouldPull := o.PullAlways || !m.mounter.ImageExists(o.Context, o.NamedRef)
		if shouldPull {
			klog.Infof("pull image %q ", o.Image)
			pullstatus.Update(namedRef, pullstatus.StillPulling)
			startTime := time.Now()
			if err = puller.Pull(o.Context); err != nil {
				pullstatus.Update(namedRef, pullstatus.Errored)
				metrics.OperationErrorsCount.WithLabelValues("StartPulling").Inc()
				return errors.Errorf("unable to pull image %q: %s", o.NamedRef, err)
			}
			metrics.ImagePullTime.WithLabelValues(metrics.Sync).Observe(time.Since(startTime).Seconds())
		}
		pullstatus.Update(namedRef, pullstatus.Pulled)
		return nil
	}

	if pullstatus.Get(namedRef) == pullstatus.Pulled ||
		pullstatus.Get(namedRef) == pullstatus.StillPulling {
		return nil
	}

	go func() {
		if pullstatus.Get(namedRef) == pullstatus.StatusNotFound {
			m.mutex.Lock()
			defer m.mutex.Unlock()
			c, cancel := context.WithTimeout(context.Background(), pullCtxTimeout)
			defer cancel()

			if pullstatus.Get(namedRef) == pullstatus.StillPulling ||
				pullstatus.Get(namedRef) == pullstatus.Pulled {
				return
			}

			puller := remoteimage.NewPuller(m.imageSvcClient, o.NamedRef, keyring)
			shouldPull := o.PullAlways || !m.mounter.ImageExists(o.Context, o.NamedRef)
			if shouldPull {
				if m.tokens != nil {
					m.tokens <- struct{}{}
					defer func() {
						<-m.tokens
					}()
				}
				klog.Infof("pull image %q ", o.Image)
				pullstatus.Update(namedRef, pullstatus.StillPulling)
				startTime := time.Now()

				if err = puller.Pull(c); err != nil {
					pullstatus.Update(namedRef, pullstatus.Errored)
					metrics.OperationErrorsCount.WithLabelValues("StartPulling").Inc()
					m.asyncErrs[namedRef] = fmt.Errorf("unable to pull image %q: %s", o.Image, err)
					return
				}
				metrics.ImagePullTime.WithLabelValues(metrics.Async).Observe(time.Since(startTime).Seconds())
			}
			klog.Infof("image is ready for use: %q ", o.Image)
			pullstatus.Update(namedRef, pullstatus.Pulled)
		}
	}()

	return nil
}

// WaitForPull waits until the image pull succeeds or errors or timeout is exceeded
func (m *PullExecutor) WaitForPull(o *PullOptions) error {
	if !m.asyncPull {
		return nil
	}

	namedRef := o.NamedRef.String()
	condFn := func() (done bool, err error) {
		if pullstatus.Get(namedRef) == pullstatus.Pulled {
			return true, nil
		}

		if m.asyncErrs[namedRef] != nil {
			return false, m.asyncErrs[namedRef]
		}
		return false, nil
	}

	if err := wait.PollImmediate(
		pullPollTimeInterval,
		pullPollTimeout,
		condFn); err != nil {
		return errors.Errorf("waited too long to download the image: %v", err)
	}

	return nil
}
