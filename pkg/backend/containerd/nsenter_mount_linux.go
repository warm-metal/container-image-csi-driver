//go:build linux

package containerd

// Re-exec-based syscall.Mount for host mount namespace.
//
// Problem: util-linux 2.39+ uses the fd-based mount API (fsopen/fsconfig/fsmount).
// On kernel 6.12 (Bottlerocket 1.59), fsconfig(lowerdir=...) returns EINVAL when the
// colon-joined lowerdir string exceeds ~256 chars — triggered by images with 3+ layers.
// The legacy mount(2) syscall is not affected. runc and containerd-shim already use it.
//
// Solution: instead of exec'ing "nsenter -- mount", re-exec the driver binary itself.
// The child detects _CSI_NSENTER_MOUNT=1 in init() (before Go's scheduler starts more
// threads), enters the host mount namespace via setns(2), calls unix.Mount directly,
// then exits. This mirrors the pattern used by runc and github.com/moby/sys/reexec.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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

	// We are the re-exec child. Lock this goroutine to its OS thread so that setns
	// affects the correct thread and is not migrated by Go's scheduler.
	runtime.LockOSThread()

	if err := runNsenterMountChild(); err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-mount child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// runNsenterMountChild is the full logic executed by the re-exec child:
// decode request → enter host mount namespace → syscall mount → exit.
func runNsenterMountChild() error {
	var req nsenterMountRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return fmt.Errorf("decode mount request: %w", err)
	}

	// Enter the host mount namespace via the bind-mount at /host/proc/1/ns/mnt.
	f, err := os.Open(hostMountNS)
	if err != nil {
		return fmt.Errorf("open host mount namespace %s: %w", hostMountNS, err)
	}
	defer f.Close()

	if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNS); err != nil {
		return fmt.Errorf("setns into host mount namespace: %w", err)
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

// syscallMountInHostNamespace re-execs the driver binary to perform a single
// mount(2) syscall inside the host mount namespace. This avoids util-linux's
// fd-based mount API (fsopen/fsconfig) which fails on kernel 6.12 (Bottlerocket 1.59)
// when the overlay lowerdir string exceeds ~256 chars (EINVAL via fsconfig).
//
// The re-exec pattern is required because setns(2) is per-thread: re-execing into a
// fresh single-threaded process makes it safe to call setns before Go's scheduler
// starts additional threads. This is the same approach used by runc.
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

	cmd := exec.Command(self)
	cmd.Env = []string{envNsenterMount + "=1"}
	cmd.Stdin = bytes.NewReader(payload)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount failed: %w, output: %s", err, string(out))
	}

	klog.V(4).Infof("mounted %q → %q (type=%q, opts=%v) via syscall.Mount re-exec",
		source, target, fstype, options)
	return nil
}
