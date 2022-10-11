package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mitchellh/go-ps"
	"github.com/spf13/pflag"
)

func detectKubeletProcess() *driverConfig {
	procs, err := ps.Processes()
	if err != nil {
		panic(err)
	}

	k3s := false
	pid := 0
	for _, proc := range procs {
		if proc.Executable() == "kubelet" {
			pid = proc.Pid()
			break
		}

		if proc.Executable() == "k3s-server" {
			k3s = true
			break
		}
	}

	if k3s {
		return &driverConfig{
			KubeletRoot:       "/var/lib/kubelet",
			Runtime:           Containerd,
			RuntimeSocketPath: "/run/k3s/containerd/containerd.sock",
			ImageSocketPath:   "/run/k3s/containerd/containerd.sock",
		}
	}

	if pid == 0 {
		fmt.Fprintln(os.Stderr, "kubelet process not found")
	}

	kubeletCmdLine, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		panic(err)
	}

	var kubeletArgs []string
	for _, byt := range bytes.Split(kubeletCmdLine, []byte{0x00}) {
		kubeletArgs = append(kubeletArgs, string(byt))
	}

	flags := pflag.NewFlagSet("kubelet", pflag.PanicOnError)
	flags.ParseErrorsWhitelist.UnknownFlags = true

	rootDirectory := "/var/lib/kubelet"
	containerRuntime := "docker"
	remoteRuntimeEndpoint := ""
	remoteImageEndpoint := ""
	flags.StringVar(&rootDirectory, "root-dir", rootDirectory, "")
	flags.StringVar(&containerRuntime, "container-runtime", containerRuntime, "")
	flags.StringVar(&remoteRuntimeEndpoint, "container-runtime-endpoint", remoteRuntimeEndpoint, "")
	flags.StringVar(&remoteImageEndpoint, "image-service-endpoint", remoteImageEndpoint, "")
	flags.Parse(kubeletArgs)

	if containerRuntime == "docker" {
		fmt.Fprintln(os.Stderr, "Found container runtime docker. Assuming containerd enabled.")
		remoteRuntimeEndpoint = "unix:///run/containerd/containerd.sock"
	}

	if remoteRuntimeEndpoint == "" && remoteImageEndpoint == "" {
		fmt.Fprintln(os.Stderr, "container runtime not found")
		return nil
	}

	if remoteImageEndpoint == "" {
		remoteImageEndpoint = remoteRuntimeEndpoint
	}

	imageSocketPath, err := url.Parse(remoteImageEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid image service socket %s\n", remoteImageEndpoint)
		return nil
	}

	runtimeSocketPath, err := url.Parse(remoteRuntimeEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid runtime socket %s\n", remoteRuntimeEndpoint)
		return nil
	}

	var runtime ContainerRuntime
	if strings.HasSuffix(remoteRuntimeEndpoint, "containerd.sock") {
		runtime = Containerd
	} else if strings.HasSuffix(remoteRuntimeEndpoint, "crio.sock") {
		runtime = CriO
	} else {
		fmt.Fprintf(os.Stderr, "unknown container runtime %s\n", remoteRuntimeEndpoint)
		return nil
	}

	return &driverConfig{
		KubeletRoot:       rootDirectory,
		Runtime:           runtime,
		RuntimeSocketPath: runtimeSocketPath.Path,
		ImageSocketPath:   imageSocketPath.Path,
	}
}
