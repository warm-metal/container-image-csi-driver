# Overlay Mount Fix: Re-exec with syscall.Mount

## Problem

On Linux kernels 6.5+ with util-linux 2.39+, mounting overlayfs via the `mount` binary
fails with `exit status 32` ("wrong fs type, bad option, bad superblock on overlay") when
the image has 3 or more layers.

The failure is in util-linux's fd-based mount API (`fsopen`/`fsconfig`/`fsmount`),
introduced in util-linux 2.39. When `fsconfig` receives the colon-joined `lowerdir`
string for 3+ layers, it returns `EINVAL` because the string exceeds an effective ~256-char
limit in the kernel's overlayfs `fs_context` parameter handling.

The legacy `mount(2)` syscall does not have this limit and is unaffected.

Tracking:
- [util-linux/util-linux#2287](https://github.com/util-linux/util-linux/issues/2287) — open since Jun 2023, no fix
- [spkenv/spk#968](https://github.com/spkenv/spk/issues/968) — confirms 256-char limit via strace

## Fix

Replace `nsenter --mount=... -- mount` (which invokes the `mount` binary) with a re-exec
of the driver binary that calls `unix.Mount` (legacy `mount(2)`) directly after entering
the host mount namespace via `setns(2)`.

**Why re-exec rather than calling `unix.Mount` in-process?**

`setns(2)` is per-thread. Go's scheduler multiplexes goroutines across OS threads, so
calling `setns` in one goroutine gives no guarantee that the subsequent `unix.Mount` call
runs on the same thread. Re-execing into a fresh, single-threaded process makes it safe to
call `setns` before Go's scheduler creates additional threads. This is the same pattern
used by runc.

## Implementation

**`pkg/backend/containerd/nsenter_mount_linux.go`** (new)

- `init()`: detects `_CSI_NSENTER_MOUNT=1`, locks to OS thread, opens
  `/host/proc/1/ns/mnt`, calls `unix.Setns`, then `unix.Mount`, and exits.
- `syscallMountInHostNamespace()`: JSON-encodes the mount request, re-execs the driver
  binary with `_CSI_NSENTER_MOUNT=1` and the payload on stdin.

**`pkg/backend/containerd/containerd.go`**

- `mountInHostNamespace()` now calls `syscallMountInHostNamespace()` instead of
  exec'ing `nsenter -- mount`.
- SELinux `context=...` injection is preserved and applied before the call.
- `unmountInHostNamespace()` is unchanged (unmount does not use fsconfig).
