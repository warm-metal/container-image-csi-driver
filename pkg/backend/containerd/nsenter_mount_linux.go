//go:build linux

package containerd

// Re-exec-based syscall.Mount for host mount namespace.
//
// Problem: util-linux 2.39+ uses the fd-based mount API (fsopen/fsconfig/fsmount).
// On kernel 6.12 (Bottlerocket 1.59), fsconfig(lowerdir=...) returns EINVAL when the
// colon-joined lowerdir string exceeds ~256 chars — triggered by images with 3+ layers.
// The legacy mount(2) syscall is not affected. runc and containerd-shim already use it.
//
// Solution: use nsenter to enter the host mount namespace, then exec the mount-helper
// binary from the CSI socket-dir hostPath volume. The helper detects _CSI_NSENTER_MOUNT=1,
// calls unix.Mount directly, and exits.
//
// The mount-helper binary is a copy of the driver binary, placed on the hostPath by an
// initContainer in the DaemonSet. After nsenter switches mount namespace, paths on the
// container's overlay rootfs are not accessible, but hostPath volumes exist on the host
// filesystem and remain accessible from both namespaces.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const (
	// envNsenterMount is the sentinel env var that triggers the re-exec child path.
	envNsenterMount = "_CSI_NSENTER_MOUNT"
	// hostMountNS is the bind-mounted host mount namespace inside the driver container.
	hostMountNS = "/host/proc/1/ns/mnt"
	// mountHelperName is the name of the helper binary on the hostPath volume.
	mountHelperName = "mount-helper"
)

// nsenterMountRequest is the payload passed from parent to child via stdin (JSON).
type nsenterMountRequest struct {
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	FSType  string   `json:"fstype"`
	Options []string `json:"options"`
}

func init() {
	if os.Getenv(envNsenterMount) != "1" {
		return
	}

	// We are the re-exec child running in the host mount namespace.
	// Decode the request from stdin and call unix.Mount directly.
	if err := runNsenterMountChild(); err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-mount child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runNsenterMountChild() error {
	var req nsenterMountRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return fmt.Errorf("decode mount request: %w", err)
	}

	data := strings.Join(req.Options, ",")

	if err := unix.Mount(req.Source, req.Target, req.FSType, 0, data); err != nil {
		return fmt.Errorf("mount(%q → %q, type=%q, data=%q): %w",
			req.Source, req.Target, req.FSType, data, err)
	}

	return nil
}

// csiSocketDir returns the host-side path of the CSI socket directory.
// This directory is a hostPath volume visible from both the container and host namespaces.
// The initContainer copies the driver binary here as "mount-helper" before the main
// container starts.
func csiSocketDir() string {
	if v := os.Getenv("CSI_SOCKET_DIR"); v != "" {
		return v
	}
	// Default: matches the helm chart's socket-dir hostPath.
	return "/var/lib/kubelet/plugins/csi-image.warm-metal.tech"
}

// syscallMountInHostNamespace uses nsenter to enter the host mount namespace and then
// execs the mount-helper binary (placed on the socket-dir hostPath by the initContainer)
// to call unix.Mount (legacy mount(2)) directly.
func syscallMountInHostNamespace(source, target, fstype string, options []string) error {
	hostHelper := filepath.Join(csiSocketDir(), mountHelperName)

	req := nsenterMountRequest{
		Source:  source,
		Target:  target,
		FSType:  fstype,
		Options: options,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal mount request: %w", err)
	}

	// nsenter enters the host mount namespace, then execs the helper binary from the
	// hostPath volume (accessible from both container and host namespaces).
	cmd := exec.Command("nsenter", "--mount="+hostMountNS, "--", hostHelper)
	cmd.Env = []string{envNsenterMount + "=1"}
	cmd.Stdin = bytes.NewReader(payload)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount failed: %w, output: %s", err, string(out))
	}

	klog.V(4).Infof("mounted %q → %q (type=%q, opts=%v) via nsenter+syscall.Mount",
		source, target, fstype, options)
	return nil
}
