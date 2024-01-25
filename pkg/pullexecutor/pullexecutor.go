package pullexecutor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"github.com/pkg/errors"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/errorstore"
	"github.com/warm-metal/csi-driver-image/pkg/metrics"
	"github.com/warm-metal/csi-driver-image/pkg/remoteimage"
	"github.com/warm-metal/csi-driver-image/pkg/secret"
	s "github.com/warm-metal/csi-driver-image/pkg/status"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

// PullExecutorOptions are the options passed to the pull executor
type PullExecutorOptions struct {
	AsyncPull          bool
	ImageServiceClient cri.ImageServiceClient
	SecretStore        secret.Store
	Mounter            backend.Mounter
	OverrideTimeout    *time.Duration
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
	timeout        *time.Duration
}

// NewPullExecutor initializes a new pull executor object
func NewPullExecutor(o *PullExecutorOptions) *PullExecutor {
	return &PullExecutor{
		asyncPull:      o.AsyncPull,
		mutexes:        make(map[string]*sync.Mutex),
		imageSvcClient: o.ImageServiceClient,
		secretStore:    o.SecretStore,
		mounter:        o.Mounter,
		errorStore:     errorstore.New(),
		timeout:        o.OverrideTimeout,
	}
}

// StartPulling starts pulling the image
func (m *PullExecutor) StartPulling(o *PullOptions) error {
	nref := o.NamedRef.String()
	pullStatusKey := s.CompositeKey(nref, o.PodUid)
	if m.mutexes[pullStatusKey] == nil {
		m.mutexes[pullStatusKey] = &sync.Mutex{}
	}

	keyring, err := m.secretStore.GetDockerKeyring(o.Context, o.PullSecrets)
	if err != nil {
		return errors.Errorf("unable to fetch keyring: %s", err)
	}

	defer delete(m.mutexes, pullStatusKey)
	fmt.Println("o, keyring, nref, pullStatusKey", o, keyring, nref, pullStatusKey)
	e := m.pullImage(o, keyring, nref, pullStatusKey)

	return e
}

func (m *PullExecutor) pullImage(o *PullOptions,
	keyring credentialprovider.DockerKeyring, namedRef, pullStatusKey string) error {

	m.mutexes[pullStatusKey].Lock()
	defer m.mutexes[pullStatusKey].Unlock()

	if s.PullStatus.Get(pullStatusKey) == s.Processed ||
		s.PullStatus.Get(pullStatusKey) == s.StillProcessing {

		fmt.Println("o.PodUid", o.PodUid, namedRef)
		e := errors.Errorf("image '%v' for pod uid '%v' is being pulled or already pulled", namedRef, o.PodUid)
		fmt.Println("ERROR", e)
		// klog.Infof("image %q for pod uid %q is being pulled or already pulled", namedRef, o.PodUid)
		return e
	}

	c := o.Context
	var cancel context.CancelFunc
	if m.timeout != nil {
		c, cancel = context.WithTimeout(context.Background(), *m.timeout)
		defer cancel()
	}

	puller := remoteimage.NewPuller(m.imageSvcClient, o.NamedRef, keyring)
	shouldPull := o.PullAlways || !m.mounter.ImageExists(o.Context, o.NamedRef)
	if shouldPull {
		klog.Infof("pull image %q ", o.Image)
		s.PullStatus.Update(pullStatusKey, s.StillProcessing)
		startTime := time.Now()
		if err := puller.Pull(c); err != nil {
			s.PullStatus.Update(pullStatusKey, s.Errored)
			metrics.OperationErrorsCount.WithLabelValues("StartPulling").Inc()
			return fmt.Errorf("unable to pull image %q: %s", o.NamedRef, err)
		}
		metrics.ImagePullTime.Observe(time.Since(startTime).Seconds())
	}
	klog.Infof("image is ready for use: %q ", o.NamedRef)
	s.PullStatus.Update(pullStatusKey, s.Processed)
	return nil
}
