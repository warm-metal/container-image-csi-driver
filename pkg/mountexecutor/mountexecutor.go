package mountexecutor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/containerd/containerd/reference/docker"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
	"github.com/warm-metal/csi-driver-image/pkg/constants"
	"github.com/warm-metal/csi-driver-image/pkg/errorstore"
	"github.com/warm-metal/csi-driver-image/pkg/metrics"
	"github.com/warm-metal/csi-driver-image/pkg/mountstatus"
	"github.com/warm-metal/csi-driver-image/pkg/pullstatus"
	"k8s.io/apimachinery/pkg/util/wait"
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
	namedRef := o.NamedRef.String()
	if m.mutexes[o.TargetPath] == nil {
		m.mutexes[o.TargetPath] = &sync.Mutex{}
	}

	if pullstatus.Get(namedRef) != pullstatus.Pulled ||
		mountstatus.Get(o.TargetPath) == mountstatus.StillMounting ||
		mountstatus.Get(o.TargetPath) == mountstatus.Mounted {
		klog.Infof("can't mount the image '%s' (image pull status: %q; volume mount status: %q)",
			o.NamedRef.Name(),
			pullstatus.Get(o.NamedRef), mountstatus.Get(o.TargetPath))
		return nil
	}

	ro := o.ReadOnly ||
		o.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
		o.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY

	if !m.asyncMount {
		return m.mount(o, constants.Sync, ro)
	}

	go func() error {
		return m.mount(o, constants.Async, ro)
	}()

	return nil
}

func (m *MountExecutor) mount(o *MountOptions, mountType string, ro bool) error {

	ctx := o.Context
	var cancel context.CancelFunc

	if mountType == constants.Async {
		ctx, cancel = context.WithTimeout(context.Background(), mountCtxTimeout)
		defer cancel()
	}

	m.mutexes[o.TargetPath].Lock()
	defer m.mutexes[o.TargetPath].Unlock()
	mountstatus.Update(o.TargetPath, mountstatus.StillMounting)

	startTime := time.Now()
	if err := m.mounter.Mount(ctx, o.VolumeId, backend.MountTarget(o.TargetPath), o.NamedRef, ro); err != nil {
		klog.Errorf("mount err: %v", err.Error())
		metrics.OperationErrorsCount.WithLabelValues("StartMounting").Inc()
		mountstatus.Update(o.TargetPath, mountstatus.Errored)
		return m.errorStore.Put(o.TargetPath, err)
	}
	metrics.ImageMountTime.WithLabelValues(mountType).Observe(time.Since(startTime).Seconds())
	mountstatus.Update(o.TargetPath, mountstatus.Mounted)
	m.errorStore.Remove(o.TargetPath)
	return nil
}

// WaitForMount waits for the volume to get mounted
func (m *MountExecutor) WaitForMount(o *MountOptions) error {
	if pullstatus.Get(o.NamedRef) != pullstatus.Pulled {
		return nil
	}

	if !m.asyncMount {
		return nil
	}

	mountCondFn := func() (done bool, err error) {
		if mountstatus.Get(o.TargetPath) == mountstatus.Mounted {
			return true, nil
		}
		if m.errorStore.Get(o.TargetPath) != nil {
			return false, m.errorStore.Get(o.TargetPath)
		}
		return false, nil
	}

	if err := wait.PollImmediate(
		mountPollTimeInterval,
		mountPollTimeout,
		mountCondFn); err != nil {
		return fmt.Errorf("waited too long to mount the image: %v", err)
	}

	return nil
}
