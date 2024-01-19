package pullexecutor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"github.com/pkg/errors"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/constants"
	"github.com/warm-metal/csi-driver-image/pkg/errorstore"
	"github.com/warm-metal/csi-driver-image/pkg/metrics"
	"github.com/warm-metal/csi-driver-image/pkg/pullstatus"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	"k8s.io/apimachinery/pkg/util/wait"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

const (
	pullPollTimeInterval   = 100 * time.Millisecond
	DefaultPullPollTimeout = 2 * time.Minute
	pullCtxTimeout         = 10 * time.Minute
)

// PullExecutorOptions are the options passed to the pull executor
type PullExecutorOptions struct {
	AsyncPull          bool
	ImageServiceClient cri.ImageServiceClient
	SecretStore        secret.Store
	Mounter            backend.Mounter
	MaxInflightPulls   int
	AsyncPullTimeout   time.Duration
}

// PullOptions are the options for a single pull request
type PullOptions struct {
	// Context here is only valid for synchronous mounts
	Context     context.Context
	NamedRef    docker.Named
	PullAlways  bool
	PullSecrets map[string]string
	Image       string
	PodUid      string
}

// PullExecutor executes the pulls
type PullExecutor struct {
	asyncPull      bool
	imageSvcClient cri.ImageServiceClient
	mutexes        map[string]*sync.Mutex
	errorStore     *errorstore.ErrorStore
	secretStore    secret.Store
	mounter        backend.Mounter
	tokens         chan struct{}
	// only valid for async pulls
	pullTimeout time.Duration
}

// NewPullExecutor initializes a new pull executor object
func NewPullExecutor(o *PullExecutorOptions) *PullExecutor {

	var tokens chan struct{}

	if o.MaxInflightPulls > 0 {
		tokens = make(chan struct{}, o.MaxInflightPulls)
	}

	return &PullExecutor{
		asyncPull:      o.AsyncPull,
		mutexes:        make(map[string]*sync.Mutex),
		imageSvcClient: o.ImageServiceClient,
		secretStore:    o.SecretStore,
		mounter:        o.Mounter,
		errorStore:     errorstore.New(),
		tokens:         tokens,
		pullTimeout:    o.AsyncPullTimeout,
	}
}

// StartPulling starts pulling the image
func (m *PullExecutor) StartPulling(o *PullOptions) error {
	namedRef := o.NamedRef.String()
	pullStatusKey := pullstatus.Key(namedRef, o.PodUid)
	if m.mutexes[pullStatusKey] == nil {
		m.mutexes[pullStatusKey] = &sync.Mutex{}
	}

	keyring, err := m.secretStore.GetDockerKeyring(o.Context, o.PullSecrets)
	if err != nil {
		return errors.Errorf("unable to fetch keyring: %s", err)
	}

	if !m.asyncPull {
		return m.pullImage(o, keyring, constants.Sync, namedRef, pullStatusKey)
	}

	if pullstatus.Get(pullStatusKey) == pullstatus.Pulled ||
		pullstatus.Get(pullStatusKey) == pullstatus.StillPulling {
		return nil
	}

	go func() error {
		return m.pullImage(o, keyring, constants.Sync, namedRef, pullStatusKey)
	}()

	return nil
}

func (m *PullExecutor) pullImage(o *PullOptions,
	keyring credentialprovider.DockerKeyring,
	pullType, namedRef, pullStatusKey string) error {
	c := o.Context
	var cancel context.CancelFunc
	if pullstatus.Get(pullStatusKey) == pullstatus.StatusNotFound {
		if pullType == constants.Async {
			c, cancel = context.WithTimeout(context.Background(), pullCtxTimeout)
			defer cancel()
		}

		m.mutexes[pullStatusKey].Lock()
		defer m.mutexes[pullStatusKey].Unlock()

		if pullstatus.Get(pullStatusKey) == pullstatus.StillPulling {
			klog.Infof("image %q for pod uid %q is already being pulled", namedRef, o.PodUid)
			return nil
		}

		puller := remoteimage.NewPuller(m.imageSvcClient, o.NamedRef, keyring)
		shouldPull := o.PullAlways || !m.mounter.ImageExists(o.Context, o.NamedRef)
		if shouldPull {
			// i.e., if max-in-flight-pulls is set to a non-zero value
			if len(m.tokens) > 0 {
				if m.tokens != nil {
					m.tokens <- struct{}{}
					defer func() {
						<-m.tokens
					}()
				}
			}
			klog.Infof("pull image %q ", o.Image)
			pullstatus.Update(pullStatusKey, pullstatus.StillPulling)
			startTime := time.Now()
			if err := puller.Pull(c); err != nil {
				pullstatus.Update(pullStatusKey, pullstatus.Errored)
				metrics.OperationErrorsCount.WithLabelValues("StartPulling").Inc()
				return m.errorStore.Put(pullStatusKey, fmt.Errorf("unable to pull image %q: %s", o.NamedRef, err))
			}
			metrics.ImagePullTime.WithLabelValues(pullType).Observe(time.Since(startTime).Seconds())
		}
		klog.Infof("image is ready for use: %q ", o.NamedRef)
		pullstatus.Update(pullStatusKey, pullstatus.Pulled)
		m.errorStore.Remove(pullStatusKey)
		return nil
	}
	return nil
}

// WaitForPull waits until the image pull succeeds or errors or timeout is exceeded
func (m *PullExecutor) WaitForPull(o *PullOptions) error {
	if !m.asyncPull {
		return nil
	}

	namedRef := o.NamedRef.String()
	pullStatusKey := pullstatus.Key(namedRef, o.PodUid)
	condFn := func() (done bool, err error) {
		if pullstatus.Get(pullStatusKey) == pullstatus.Pulled {
			return true, nil
		}

		if m.errorStore.Get(pullStatusKey) != nil {
			return false, m.errorStore.Get(pullStatusKey)
		}
		return false, nil
	}

	if err := wait.PollImmediate(
		pullPollTimeInterval,
		m.pullTimeout,
		condFn); err != nil {
		return errors.Errorf("waited too long to download the image: %v", err)
	}

	return nil
}
