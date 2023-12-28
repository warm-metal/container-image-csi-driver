package pullexecutor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"github.com/pkg/errors"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/pullstatus"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	"k8s.io/apimachinery/pkg/util/wait"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
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
	Mounter            *backend.SnapshotMounter
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
	asyncErrs      map[docker.Named]error
	secretStore    secret.Store
	mounter        *backend.SnapshotMounter
}

// NewPullExecutor initializes a new pull executor object
func NewPullExecutor(o *PullExecutorOptions) *PullExecutor {
	return &PullExecutor{
		asyncPull:      o.AsyncPull,
		mutex:          &sync.Mutex{},
		imageSvcClient: o.ImageServiceClient,
		secretStore:    o.SecretStore,
		mounter:        o.Mounter,
	}
}

// StartPulling starts pulling the image
func (m *PullExecutor) StartPulling(o *PullOptions, valuesLogger klog.Logger) error {

	keyring, err := m.secretStore.GetDockerKeyring(o.Context, o.PullSecrets)
	if err != nil {
		return errors.Errorf("unable to fetch keyring: %s", err)
	}

	if !m.asyncPull {
		puller := remoteimage.NewPuller(m.imageSvcClient, o.NamedRef, keyring)
		shouldPull := o.PullAlways || !m.mounter.ImageExists(o.Context, o.NamedRef)
		if shouldPull {
			valuesLogger.Info(fmt.Sprintf("pull image %q ", o.Image))
			pullstatus.Update(o.NamedRef, pullstatus.StillPulling)
			start := time.Now()
			if err = puller.Pull(o.Context); err != nil {
				pullstatus.Update(o.NamedRef, pullstatus.Errored)
				return errors.Errorf("unable to pull image %q: %s", o.NamedRef, err)
			}
			elapsed := time.Since(start)
			valuesLogger.Info(fmt.Sprintf("pulling %q took %s", o.Image, elapsed))
			valuesLogger.Info("getting size")
			size, _ := puller.ImageSize(o.Context)
			valuesLogger.Info(fmt.Sprintf("image size: %d", size))
		}
		pullstatus.Update(o.NamedRef, pullstatus.Pulled)
		return nil
	}

	if pullstatus.Get(o.NamedRef) == pullstatus.Pulled ||
		pullstatus.Get(o.NamedRef) == pullstatus.StillPulling {
		return nil
	}

	go func() {
		if pullstatus.Get(o.NamedRef) == pullstatus.StatusNotFound {
			m.mutex.Lock()
			defer m.mutex.Unlock()
			c, cancel := context.WithTimeout(context.Background(), pullCtxTimeout)
			defer cancel()

			if pullstatus.Get(o.NamedRef) == pullstatus.StillPulling {
				return
			}

			puller := remoteimage.NewPuller(m.imageSvcClient, o.NamedRef, keyring)
			shouldPull := o.PullAlways || !m.mounter.ImageExists(o.Context, o.NamedRef)
			if shouldPull {
				valuesLogger.Info(fmt.Sprintf("pull image %q ", o.Image))
				pullstatus.Update(o.NamedRef, pullstatus.StillPulling)
				start := time.Now()
				if err = puller.Pull(c); err != nil {
					pullstatus.Update(o.NamedRef, pullstatus.Errored)
					m.asyncErrs[o.NamedRef] = fmt.Errorf("unable to pull image %q: %s", o.Image, err)
					return
				}
				elapsed := time.Since(start)
				valuesLogger.Info(fmt.Sprintf("pulling %q took %s", o.Image, elapsed))
				valuesLogger.Info("getting size")
				size, _ := puller.ImageSize(o.Context)
				valuesLogger.Info(fmt.Sprintf("image size: %d", size))
			}
			pullstatus.Update(o.NamedRef, pullstatus.Pulled)
		}
	}()

	return nil
}

// WaitForPull waits until the image pull succeeds or errors or timeout is exceeded
func (m *PullExecutor) WaitForPull(o *PullOptions) error {
	if !m.asyncPull {
		return nil
	}

	condFn := func() (done bool, err error) {
		if pullstatus.Get(o.NamedRef) == pullstatus.Pulled {
			return true, nil
		}

		if m.asyncErrs[o.NamedRef] != nil {
			return false, m.asyncErrs[o.NamedRef]
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
