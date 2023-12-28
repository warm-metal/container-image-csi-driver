package mountexecutor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/containerd/containerd/reference/docker"
	"github.com/warm-metal/csi-driver-image/pkg/backend"
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
	Mounter    *backend.SnapshotMounter
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
	mutex      *sync.Mutex
	mounter    *backend.SnapshotMounter
	asyncErrs  map[docker.Named]error
}

// NewMountExecutor initializes a new mount executor
func NewMountExecutor(o *MountExecutorOptions) *MountExecutor {
	return &MountExecutor{
		asyncMount: o.AsyncMount,
		mutex:      &sync.Mutex{},
		mounter:    o.Mounter,
	}
}

// StartMounting starts the mounting
func (m *MountExecutor) StartMounting(o *MountOptions, valuesLogger klog.Logger) error {

	if pullstatus.Get(o.NamedRef) != pullstatus.Pulled || mountstatus.Get(o.VolumeId) == mountstatus.StillMounting {
		klog.Infof("image '%s' hasn't been pulled yet (status: %s) or volume is still mounting (status: %s)",
			o.NamedRef.Name(),
			pullstatus.Get(o.NamedRef), mountstatus.Get(o.VolumeId))
		return nil
	}

	ro := o.ReadOnly ||
		o.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
		o.VolumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY

	if !m.asyncMount {
		mountstatus.Update(o.VolumeId, mountstatus.StillMounting)
		start := time.Now()
		if err := m.mounter.Mount(o.Context, o.VolumeId, backend.MountTarget(o.TargetPath), o.NamedRef, ro); err != nil {
			mountstatus.Update(o.VolumeId, mountstatus.Errored)
			return err
		}
		mountstatus.Update(o.VolumeId, mountstatus.Mounted)
		elapsed := time.Since(start)
		valuesLogger.Info(fmt.Sprintf("mounting %q took %s", o.NamedRef.Name(), elapsed))
		return nil
	}

	go func() {
		m.mutex.Lock()
		defer m.mutex.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), mountCtxTimeout)
		defer cancel()

		mountstatus.Update(o.VolumeId, mountstatus.StillMounting)
		start := time.Now()
		if err := m.mounter.Mount(ctx, o.VolumeId, backend.MountTarget(o.TargetPath), o.NamedRef, ro); err != nil {
			klog.Errorf("mount err: %v", err.Error())
			mountstatus.Update(o.VolumeId, mountstatus.Errored)
			m.asyncErrs[o.NamedRef] = fmt.Errorf("err: %v: %v", err, m.asyncErrs[o.NamedRef])
			return
		}
		mountstatus.Update(o.VolumeId, mountstatus.Mounted)
		elapsed := time.Since(start)
		valuesLogger.Info(fmt.Sprintf("mounting %q took %s", o.NamedRef.Name(), elapsed))
	}()

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
		if mountstatus.Get(o.VolumeId) == mountstatus.Mounted {
			return true, nil
		}
		if m.asyncErrs[o.NamedRef] != nil {
			return false, m.asyncErrs[o.NamedRef]
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
