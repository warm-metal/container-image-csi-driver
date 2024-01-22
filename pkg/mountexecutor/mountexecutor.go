package mountexecutor

import (
	"context"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/containerd/containerd/reference/docker"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/constants"
	"github.com/warm-metal/csi-driver-image/pkg/errorstore"
	"github.com/warm-metal/csi-driver-image/pkg/metrics"
	s "github.com/warm-metal/csi-driver-image/pkg/status"
	"k8s.io/klog/v2"
)

const (
	mountPollTimeInterval = 100 * time.Millisecond
	mountPollTimeout      = 2 * time.Minute
	mountCtxTimeout       = 10 * time.Minute
)

// MountExecutorOptions are options passed to mount executor
type MountExecutorOptions struct {
	AsyncMount bool
	Mounter    backend.Mounter
}

// MountOptions are options for a single mount request
type MountOptions struct {
	// Context here is only valid for synchronous mounts
	Context          context.Context
	NamedRef         docker.Named
	VolumeId         string
	TargetPath       string
	VolumeCapability *csi.VolumeCapability
	ReadOnly         bool
	PodUid           string
}

// MountExecutor executes mount
type MountExecutor struct {
	asyncMount bool
	mutexes    map[string]*sync.Mutex
	mounter    backend.Mounter
	errorStore *errorstore.ErrorStore
}

// NewMountExecutor initializes a new mount executor
func NewMountExecutor(o *MountExecutorOptions) *MountExecutor {
	return &MountExecutor{
		asyncMount: o.AsyncMount,
		mutexes:    make(map[string]*sync.Mutex),
		mounter:    o.Mounter,
		errorStore: errorstore.New(),
	}
}

// StartMounting starts the mounting
func (m *MountExecutor) StartMounting(o *MountOptions) error {
	pullStatusKey := s.CompositeKey(o.NamedRef.String(), o.PodUid)
	if m.mutexes[o.TargetPath] == nil {
		m.mutexes[o.TargetPath] = &sync.Mutex{}
	}

	if s.PullStatus.Get(pullStatusKey) != s.Processed ||
		s.MountStatus.Get(o.TargetPath) == s.StillProcessing ||
		s.MountStatus.Get(o.TargetPath) == s.Processed {
		klog.Infof("can't mount the image '%s' (image pull status: %q; volume mount status: %q)",
			o.NamedRef.Name(),
			s.PullStatus.Get(pullStatusKey), s.MountStatus.Get(o.TargetPath))
		return nil
	}

	ro := o.ReadOnly ||
		o.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
		o.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY

	return m.mount(o, constants.Sync, ro)
}

func (m *MountExecutor) mount(o *MountOptions, mountType string, ro bool) error {
	m.mutexes[o.TargetPath].Lock()
	defer m.mutexes[o.TargetPath].Unlock()

	ctx := o.Context
	var cancel context.CancelFunc

	if mountType == constants.Async {
		ctx, cancel = context.WithTimeout(context.Background(), mountCtxTimeout)
		defer cancel()
	}

	s.MountStatus.Update(o.TargetPath, s.StillProcessing)

	startTime := time.Now()
	if err := m.mounter.Mount(ctx, o.VolumeId, backend.MountTarget(o.TargetPath), o.NamedRef, ro); err != nil {
		klog.Errorf("mount err: %v", err.Error())
		metrics.OperationErrorsCount.WithLabelValues("StartMounting").Inc()
		s.MountStatus.Update(o.TargetPath, s.Errored)
		return m.errorStore.Put(o.TargetPath, err)
	}
	metrics.ImageMountTime.WithLabelValues(mountType).Observe(time.Since(startTime).Seconds())
	s.MountStatus.Update(o.TargetPath, s.Processed)
	m.errorStore.Remove(o.TargetPath)
	return nil
}
