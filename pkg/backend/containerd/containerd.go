package containerd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/distribution/reference"
	"github.com/opencontainers/image-spec/identity"
	"github.com/warm-metal/container-image-csi-driver/pkg/backend"
	"k8s.io/klog/v2"
)

type snapshotMounter struct {
	snapshotter snapshots.Snapshotter
	cli         *client.Client
}

func NewMounter(socketPath string) backend.Mounter {
	c, err := client.New(socketPath, client.WithDefaultNamespace("k8s.io"))
	if err != nil {
		klog.Fatalf("containerd connection is broken because the mounted unix socket somehow dose not work,"+
			"recreate the container may fix: %s", err)
	}

	return backend.NewMounter(&snapshotMounter{
		snapshotter: c.SnapshotService(""),
		cli:         c,
	})
}

// selinuxContext returns the configured SELinux mount context or a safe default.
// Default: system_u:object_r:container_file_t:s0
func selinuxContext() string {
	if v := os.Getenv("CSI_SELINUX_CONTEXT"); v != "" {
		return v
	}
	return "system_u:object_r:container_file_t:s0"
}

// isSELinuxEnforcing checks host kernel SELinux enforcing state via /sys/fs/selinux/enforce.
// Returns true only when the file exists and contains "1".
func isSELinuxEnforcing() bool {
	b, err := os.ReadFile("/sys/fs/selinux/enforce")
	if err != nil {
		return false
	}

	s := strings.TrimSpace(string(b))
	return s == "1"
}

// mountInHostNamespace mounts directly in the host mount namespace using nsenter
func mountInHostNamespace(ctx context.Context, mounts []mount.Mount, target string) error {
	// Compute SELinux enforcement once per mount operation
	enforcing := isSELinuxEnforcing()
	var contextOpt string
	if enforcing {
		contextOpt = fmt.Sprintf("context=\"%s\"", selinuxContext())
	}

	// For each mount, execute it in the host namespace
	for i, m := range mounts {
		var args []string

		// Only add -t flag if type is specified
		if m.Type != "" {
			args = append(args, "-t", m.Type)
		}

		// When SELinux is enforcing on host, add a context=... option.
		mountOptions := m.Options
		if enforcing {
			alreadySet := false
			for _, opt := range mountOptions {
				if strings.HasPrefix(opt, "context=") {
					alreadySet = true
					break
				}
			}
			if !alreadySet {
				mountOptions = append(mountOptions, contextOpt)
			}
		}

		if len(mountOptions) > 0 {
			args = append(args, "-o", strings.Join(mountOptions, ","))
		}

		args = append(args, m.Source, target)

		// Build the nsenter command
		nsenterArgs := []string{"--mount=/host/proc/1/ns/mnt", "--", "mount"}
		nsenterArgs = append(nsenterArgs, args...)

		cmd := exec.CommandContext(ctx, "nsenter", nsenterArgs...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			klog.Errorf("nsenter mount failed (attempt %d/%d): %s, output: %s, command: %v",
				i+1, len(mounts), err, string(output), cmd.Args)
			return fmt.Errorf("mount failed: %w, output: %s", err, string(output))
		}
		klog.V(4).Infof("mounted %s to %s with type %s and options %v using nsenter (mount %d/%d)",
			m.Source, target, m.Type, mountOptions, i+1, len(mounts))
	}
	return nil
}

// unmountInHostNamespace unmounts directly in the host mount namespace using nsenter
func unmountInHostNamespace(ctx context.Context, target string) error {
	cmd := exec.CommandContext(ctx,
		"nsenter", "--mount=/host/proc/1/ns/mnt", "--",
		"umount", target)

	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("nsenter unmount failed: %s, output: %s", err, string(output))
		return fmt.Errorf("unmount failed: %w, output: %s", err, string(output))
	}
	klog.V(4).Infof("unmounted %s using nsenter", target)
	return nil
}

func (s snapshotMounter) Mount(ctx context.Context, key backend.SnapshotKey, target backend.MountTarget, ro bool) error {
	mounts, err := s.snapshotter.Mounts(ctx, string(key))
	if err != nil {
		klog.Errorf("unable to retrieve mounts of snapshot %q: %s", key, err)
		return err
	}

	// Mount in host namespace using nsenter
	err = mountInHostNamespace(ctx, mounts, string(target))
	if err != nil {
		mountsErr := describeMounts(mounts, string(target))
		if len(mountsErr) > 0 {
			err = errors.New(mountsErr)
		}

		klog.Errorf("unable to mount snapshot %q to target %s: %s", key, target, err)
	}

	return err
}

func (s snapshotMounter) Unmount(ctx context.Context, target backend.MountTarget) error {
	if err := unmountInHostNamespace(ctx, string(target)); err != nil {
		klog.Errorf("fail to unmount %s: %s", target, err)
		return err
	}

	return nil
}

func (s snapshotMounter) ImageExists(ctx context.Context, image reference.Named) bool {
	_, err := s.cli.GetImage(ctx, image.String())
	return err == nil
}

func (s snapshotMounter) GetImageIDOrDie(ctx context.Context, image reference.Named) string {
	localImage, err := s.cli.GetImage(ctx, image.String())
	if err != nil {
		klog.Fatalf("unable to retrieve local image %q: %s", image, err)
	}

	if err = localImage.Unpack(ctx, ""); err != nil {
		klog.Fatalf("unable to unpack image %q: %s", image, err)
	}

	klog.Infof("image %q unpacked", image)
	diffIDs, err := localImage.RootFS(ctx)
	if err != nil {
		klog.Fatalf("unable to fetch rootfs of image %q: %s", image, err)
	}

	return identity.ChainID(diffIDs).String()
}

func (s snapshotMounter) PrepareReadOnlySnapshot(
	ctx context.Context, imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	labels := defaultSnapshotLabels()
	if metadata != nil {
		labels = withTargets(defaultSnapshotLabels(), metadata.GetTargets())
	}

	klog.Infof("create ro snapshot %q for image %q with metadata %#v", key, imageID, labels)
	info, err := s.FindSnapshot(ctx, string(key), imageID, snapshots.KindView, labels)
	if info != nil {
		return err
	}

	if _, err = s.snapshotter.View(ctx, string(key), imageID, snapshots.WithLabels(labels)); err != nil {
		klog.Errorf("unable to create read-only snapshot %q of image %q: %s", key, imageID, err)
	}

	return err
}

func (s snapshotMounter) PrepareRWSnapshot(
	ctx context.Context, imageID string, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	labels := defaultSnapshotLabels()
	if metadata != nil {
		labels = withTargets(defaultSnapshotLabels(), metadata.GetTargets())
	}

	klog.Infof("create rw snapshot %q for image %q with metadata %#v", key, imageID, labels)
	info, err := s.FindSnapshot(ctx, string(key), imageID, snapshots.KindActive, labels)
	if info != nil {
		return err
	}

	if _, err = s.snapshotter.Prepare(ctx, string(key), imageID, snapshots.WithLabels(labels)); err != nil {
		klog.Errorf("unable to create snapshot %q of image %q: %s", key, imageID, err)
	}

	return err
}

func (s snapshotMounter) FindSnapshot(
	ctx context.Context, key, parent string, kind snapshots.Kind, labels map[string]string,
) (info *snapshots.Info, err error) {
	stat, err := s.snapshotter.Stat(ctx, key)
	if err != nil {
		return
	}

	info = &stat

	if info.Kind == kind && info.Parent == parent {
		for k, v := range labels {
			if k == gcLabel {
				continue
			}

			if info.Labels[k] != v {
				err = fmt.Errorf("found existed snapshot %q with different configuration %#v", key, info)
				return
			}
		}

		klog.Infof("found existed snapshot %q, use it", key)
		return
	}

	err = fmt.Errorf("found existed snapshot %q with different configuration %#v", key, info)
	return
}

func (s snapshotMounter) UpdateSnapshotMetadata(
	ctx context.Context, key backend.SnapshotKey, metadata backend.SnapshotMetadata,
) error {
	klog.Infof("update metadata of snapshot %q to %#v", key, metadata)
	info, err := s.snapshotter.Stat(ctx, string(key))
	if err != nil {
		klog.Errorf("unable to fetch stat of snapshot %q: %s", key, err)
		return err
	}

	for k := range info.Labels {
		if strings.HasPrefix(k, labelPrefix) {
			delete(info.Labels, k)
		}
	}

	info.Labels = withTargets(info.Labels, metadata.GetTargets())
	klog.Infof("labels of snapshot %q are %#v", key, info.Labels)
	_, err = s.snapshotter.Update(ctx, info)
	if err != nil {
		klog.Errorf("unable to update metadata of snapshot %q: %s", key, err)
	}
	return err
}

func (s snapshotMounter) DestroySnapshot(ctx context.Context, key backend.SnapshotKey) error {
	klog.Infof("remove snapshot %q", key)
	err := s.snapshotter.Remove(ctx, string(key))
	if err != nil {
		klog.Errorf("unable to remove the snapshot %q: %s", key, err)
	}

	return err
}

func (s snapshotMounter) ListSnapshots(ctx context.Context) (ss []backend.SnapshotMetadata, err error) {
	err = s.snapshotter.Walk(ctx, func(ctx context.Context, info snapshots.Info) error {
		if len(info.Labels) == 0 {
			return nil
		}

		targets := make(map[backend.MountTarget]struct{}, len(info.Labels))
		for k := range info.Labels {
			// To be compatible with old snapshots(prior to v0.4.2), we must filter read-write snapshots out.
			// The read-write snapshot always has a key of leading with "csi-", while the key of a read-only snapshot
			// is its image ID.
			if strings.HasPrefix(k, volumeIdLabelPrefix) {
				if strings.HasPrefix(info.Name[len(labelPrefix)+1:], "csi-") {
					klog.Infof("rw snapshot %q with labels %#v is created by an old versioned driver, skip it",
						info.Name, info.Labels)
					targets = nil
					break
				}

				if _, err := reference.ParseNamed(info.Name[len(labelPrefix)+1:]); err == nil {
					klog.Warningf("snapshot %q with labels %#v is an old versioned snapshot used by a PV. "+
						"It will be excluded from the ro snapshot cache, but it still can be unmounted normally.",
						info.Name, info.Labels)
					targets = nil
					break
				}
			}

			if strings.HasPrefix(k, targetLabelPrefix) {
				targets[backend.MountTarget(k[len(targetLabelPrefix)+1:])] = struct{}{}
			}
		}

		if len(targets) > 0 {
			metadata := make(backend.SnapshotMetadata)
			metadata.SetSnapshotKey(info.Name)
			metadata.SetTargets(targets)
			ss = append(ss, metadata)
			klog.Infof("got ro snapshot %q with targets %#v", info.Name, targets)
		}

		return nil
	})

	if err != nil {
		klog.Errorf("unable to list snapshots: %s", err)
		return nil, err
	}

	return
}

const (
	labelPrefix         = "container-image.csi.k8s.io"
	targetLabelPrefix   = labelPrefix + "/target"
	volumeIdLabelPrefix = labelPrefix + "/id"
	gcLabel             = "containerd.io/gc.root"
)

func defaultSnapshotLabels() map[string]string {
	return map[string]string{
		gcLabel: time.Now().UTC().Format(time.RFC3339),
	}
}

func genTargetLabel(target string) string {
	return fmt.Sprintf("%s|%s", targetLabelPrefix, target)
}

func withTarget(labels map[string]string, target string) map[string]string {
	labels[genTargetLabel(target)] = "√"
	return labels
}

func withTargets(labels map[string]string, targets map[backend.MountTarget]struct{}) map[string]string {
	for target := range targets {
		withTarget(labels, string(target))
	}
	return labels
}

func describeMounts(mounts []mount.Mount, target string) string {
	prefixes := []string{
		"lowerdir=",
		"upperdir=",
		"workdir=",
	}

	var err error
	for _, m := range mounts {
		if m.Type == "overlay" {
			for _, opt := range m.Options {
				if err != nil {
					break
				}

				for _, prefix := range prefixes {
					if strings.HasPrefix(opt, prefix) {
						dirs := strings.Split(opt[len(prefix):], ":")
						for _, dir := range dirs {
							if _, err = os.Lstat(dir); err != nil {
								break
							}
						}
						break
					}
				}
			}

			if err != nil {
				break
			}

			continue
		}

		if _, err = os.Lstat(m.Source); err != nil {
			break
		}
	}

	if err != nil {
		var b strings.Builder
		b.Grow(256)
		b.WriteString("src:")
		b.WriteString(err.Error())
		return b.String()
	}

	if _, err = os.Lstat(target); err != nil {
		var b strings.Builder
		b.Grow(256)
		b.WriteString("mountpoint:")
		b.WriteString(err.Error())
		return b.String()
	}

	return ""
}
