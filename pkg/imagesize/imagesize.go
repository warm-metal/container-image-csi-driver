package imagesize

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/containerd/containerd/reference/docker"
	manifesttypes "github.com/docker/cli/cli/manifest/types"
	"github.com/docker/cli/cli/registry/client"
	"github.com/docker/distribution"
	"github.com/docker/distribution/reference"
	registrytypes "github.com/docker/docker/api/types/registry"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

const (
	ctxTimeout = 2 * time.Minute
)

type warner struct {
	recorder     record.EventRecorder
	broadcaster  record.EventBroadcaster
	clientSet    kubernetes.Interface
	maxImageSize *resource.Quantity
}

var Warner *warner

// Initialize initializes the singleton `Warner` object
func Initialize(maxImageSize *resource.Quantity) {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("unable to get cluster config: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("unable to get cluster client: %s", err)
	}

	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(1)
	eventBroadcaster.StartRecordingToSink(&typedv1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
	eventRecorder := eventBroadcaster.NewRecorder(scheme, v1.EventSource{
		Component: "warm-metal",
	})

	Warner = &warner{
		recorder:     eventRecorder,
		broadcaster:  eventBroadcaster,
		clientSet:    clientset,
		maxImageSize: maxImageSize,
	}
}

// Cleanup performs cleanup activity (like shutting down the event broadcaster)
func (w *warner) Cleanup() {
	w.broadcaster.Shutdown()
}

// Warn creates the warning event object for the pod
func (w *warner) Warn(current *resource.Quantity, pod string, namespace string) error {
	if current.Cmp(*w.maxImageSize) <= 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()
	// TODO(vadasambar): set a timeout for the context here
	po, err := w.clientSet.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		e := fmt.Errorf("failed to get pod info to log event: %v, pod: %s, namespace: %s", err, pod, namespace)
		klog.Error(e)
		return e
	}

	c := fmt.Sprintf("%.2f MiB\n", float64(current.Value()/(1024.0*1024.0)))

	w.recorder.Event(po, v1.EventTypeWarning,
		"ImageSizeTooLarge", fmt.Sprintf("Image size '%v' is too large (max image size: %v)", strings.TrimSpace(c), w.maxImageSize.String()))

	return nil
}

// GetImageSize gets the image size from the container registry
func (w *warner) GetImageSize(creds *cri.AuthConfig, image docker.Named) (*resource.Quantity, error) {
	resolver := func(ctx context.Context, index *registrytypes.IndexInfo) registrytypes.AuthConfig {
		if creds == nil {
			return registrytypes.AuthConfig{}
		}
		return registrytypes.AuthConfig{
			Username:      creds.Username,
			Password:      creds.Password,
			Auth:          creds.Auth,
			ServerAddress: creds.ServerAddress,
			IdentityToken: creds.IdentityToken,
			RegistryToken: creds.RegistryToken,
		}
	}
	c := client.NewRegistryClient(resolver, "warm-metal", false)

	return w.fetchImageSize(c, image)
}

// fetchImageSize gets the image size from the container registry
func (w *warner) fetchImageSize(c client.RegistryClient, image reference.Named) (*resource.Quantity, error) {
	var mlist []manifesttypes.ImageManifest
	imageManifest, err := c.GetManifest(context.Background(), image)
	if err == nil {
		mlist = []manifesttypes.ImageManifest{
			imageManifest,
		}

	} else {
		mlist, err = c.GetManifestList(context.Background(), image)
		if err != nil {
			return nil, fmt.Errorf("failed to get image manifest and manifest list: %v, image: %s", err, image)
		}
	}

	var parsedSize *resource.Quantity

	for _, m := range mlist {
		// TODO(vadasambar): support other OS platforms
		if m.Descriptor.Platform.OS == "linux" &&
			m.Descriptor.Platform.Architecture == "amd64" {
			var size int64

			var layers []distribution.Descriptor
			if m.OCIManifest != nil {
				layers = m.OCIManifest.Layers
			} else if m.SchemaV2Manifest != nil {
				layers = m.SchemaV2Manifest.Layers
			} else {
				return parsedSize, fmt.Errorf("both OCI and Schema2 manifests are nil for the image: '%v'", image.String())
			}

			for _, l := range layers {
				size += l.Size
			}

			parsedSize = resource.NewQuantity(size, resource.BinarySI)
		}
	}

	if parsedSize == nil {
		return parsedSize, fmt.Errorf("couldn't get image size for the image '%v' (there might be no image available for linux/amd64 architecture)", image.String())
	}
	return parsedSize, nil
}
