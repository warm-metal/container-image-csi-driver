package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	criapis "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
)

var typeDir = corev1.HostPathDirectory
var typeFile = corev1.HostPathFile

func detectImageSvcVolumes(imageSvcSocketPath string) []corev1.Volume {
	socketUrl := url.URL{
		Scheme: "unix",
		Path:   imageSvcSocketPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, socketUrl.String(), grpc.WithInsecure())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to connect to the CRI image service via %v: %s\n", socketUrl, err)
		return nil
	}

	imgSvc := criapis.NewImageServiceClient(conn)
	resp, err := imgSvc.ImageFsInfo(context.TODO(), &criapis.ImageFsInfoRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to fetch image fsinfo via %s: %s\n", socketUrl.String(), err)
		return nil
	}

	var vols []corev1.Volume
	for i, fs := range resp.ImageFilesystems {
		if fs.FsId == nil {
			continue
		}

		vols = append(vols, corev1.Volume{
			Name: fmt.Sprintf("snapshot-root-%d", i),
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fs.FsId.Mountpoint,
					Type: &typeDir,
				},
			},
		})
	}

	return vols
}

type crioRootConfig struct {
	Crio struct {
		// Root is a path to the "root directory" where data not
		// explicitly handled by other options will be stored.
		Root string `toml:"root"`

		// RunRoot is a path to the "run directory" where state information not
		// explicitly handled by other options will be stored.
		RunRoot string `toml:"runroot"`

		StorageOption []string `toml:"storage_option"`
	} `toml:"crio"`
}

func fetchCriOVolumes(socketPath string) []corev1.Volume {
	cli := &http.Client{Transport: &http.Transport{
		DisableCompression: true,
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 32*time.Second)
		},
	}}

	req, err := http.NewRequest("GET", "/config", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to fetch crio configuration: %s", err)
		return nil
	}

	req.Host = "crio"
	req.URL.Host = socketPath
	req.URL.Scheme = "http"

	resp, err := cli.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "unable to fetch crio configuration: %s", err)
		return nil
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to fetch crio configuration: %s", err)
		return nil
	}

	c := &crioRootConfig{}
	if _, err = toml.Decode(string(body), c); err != nil {
		fmt.Fprintf(os.Stderr, "unable to fetch crio configuration: %s", err)
		return nil
	}

	var vols []corev1.Volume
	if c.Crio.Root != "" {
		vols = append(vols, corev1.Volume{
			Name: "crio-root",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: c.Crio.Root,
					Type: &typeDir,
				},
			},
		})
	}

	if c.Crio.RunRoot != "" {
		vols = append(vols, corev1.Volume{
			Name: "crio-run-root",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: c.Crio.RunRoot,
					Type: &typeDir,
				},
			},
		})
	}

	const overlayfsPrefix = "overlay.mount_program="
	for _, opt := range c.Crio.StorageOption {
		opt = strings.Trim(opt, `"`)
		if strings.HasPrefix(opt, overlayfsPrefix) {
			program := opt[len(overlayfsPrefix):]
			vols = append(vols, corev1.Volume{
				Name: filepath.Base(program),
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: program,
						Type: &typeFile,
					},
				},
			})
		}
	}

	return vols
}
