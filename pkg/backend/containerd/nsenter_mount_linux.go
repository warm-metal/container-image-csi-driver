//go:build linux

package containerd

// Re-exec-based syscall.Mount for host mount namespace.
//
// Problem: util-linux 2.39+ uses the fd-based mount API (fsopen/fsconfig/fsmount).
// On kernel 6.12 (Bottlerocket 1.59), fsconfig(lowerdir=...) returns EINVAL when the
// colon-joined lowerdir string exceeds ~256 chars — triggered by images with 3+ layers.
// The legacy mount(2) syscall is not affected. runc and containerd-shim already use it.
//
// Solution: use nsenter to enter the host mount namespace (nsenter handles this in a C
// process before exec), then re-exec the driver binary with _CSI_NSENTER_MOUNT=1. The
// child is already in the host mount namespace when it starts, so no setns is needed —
// it calls unix.Mount directly and exits. This avoids the setns(CLONE_NEWNS) + Go
// multi-threading incompatibility that makes in-process namespace switching unreliable.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const (
	// envNsenterMount is the sentinel env var that triggers the re-exec child path.
	envNsenterMount = "_CSI_NSENTER_MOUNT"
	// hostMountNS is the bind-mounted host mount namespace inside the driver container.
	hostMountNS = "/host/proc/1/ns/mnt"
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

	// We are the re-exec child. nsenter already placed us in the host mount namespace
	// before exec'ing this binary, so no setns is required here.
	if err := runNsenterMountChild(); err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-mount child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// runNsenterMountChild is the logic executed by the re-exec child.
// At this point the process is already in the host mount namespace (nsenter entered it).
// Decode the request from stdin and call unix.Mount directly.
func runNsenterMountChild() error {
	var req nsenterMountRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return fmt.Errorf("decode mount request: %w", err)
	}

	// Join options into a single comma-separated data string for the legacy mount(2).
	// This bypasses libmount's fsconfig path entirely.
	data := strings.Join(req.Options, ",")

	if err := unix.Mount(req.Source, req.Target, req.FSType, 0, data); err != nil {
		return fmt.Errorf("mount(%q → %q, type=%q, data=%q): %w",
			req.Source, req.Target, req.FSType, data, err)
	}

	return nil
}

// syscallMountInHostNamespace uses nsenter to enter the host mount namespace and then
// re-execs the driver binary to call unix.Mount (legacy mount(2)) directly.
//
// Why nsenter + re-exec instead of nsenter + mount binary:
//   The mount binary (util-linux 2.39+) uses the fd-based API (fsopen/fsconfig/fsmount)
//   which returns EINVAL on kernel 6.12 when the overlay lowerdir string exceeds ~256
//   chars. unix.Mount (legacy mount(2) syscall) has no such limit.
//
// Why nsenter for namespace entry instead of setns in-process:
//   setns(CLONE_NEWNS) on a multi-threaded process returns EINVAL. Go programs are
//   multi-threaded from startup. nsenter is a C program that calls setns before exec,
//   so the target binary starts already in the correct namespace — no setns needed.
func syscallMountInHostNamespace(source, target, fstype string, options []string) error {
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

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve driver executable path: %w", err)
	}

	// nsenter enters the host mount namespace (in C, before exec), then execs our binary.
	// The child starts already in the host namespace and calls unix.Mount directly.
	cmd := exec.Command("nsenter", "--mount="+hostMountNS, "--", self)
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
