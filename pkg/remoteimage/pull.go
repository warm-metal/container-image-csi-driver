package remoteimage

import (
	"context"
	"fmt"
	"time"

	"github.com/distribution/reference"
	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	"github.com/warm-metal/container-image-csi-driver/pkg/secret"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// Puller defines the interface for pulling container images
type Puller interface {
	// Pull downloads the container image
	Pull(context.Context) error
	// ImageWithTag returns the full image name with tag
	ImageWithTag() string
	// ImageWithoutTag returns the image name without tag
	ImageWithoutTag() string
	// ImageSize returns the size of the image in bytes
	ImageSize(context.Context) (int, error)
}

// NewPuller creates a new image puller instance
func NewPuller(imageSvc cri.ImageServiceClient, image reference.Named,
	keyring secret.DockerKeyring) Puller {
	return &puller{
		imageSvc: imageSvc,
		image:    image,
		keyring:  keyring,
	}
}

// puller implements the Puller interface
type puller struct {
	imageSvc cri.ImageServiceClient
	image    reference.Named
	keyring  secret.DockerKeyring
}

// ImageWithTag returns the full image name with tag
func (p puller) ImageWithTag() string {
	return p.image.String()
}

// ImageWithoutTag returns the image name without tag
func (p puller) ImageWithoutTag() string {
	return p.image.Name()
}

// Returns the compressed size of the image that was pulled in bytes
// see https://github.com/containerd/containerd/issues/9261
func (p puller) ImageSize(ctx context.Context) (int, error) {
	imageSpec := &cri.ImageSpec{Image: p.ImageWithTag()}
	imageStatusResponse, err := p.imageSvc.ImageStatus(ctx, &cri.ImageStatusRequest{
		Image: imageSpec,
	})

	if err != nil {
		metrics.OperationErrorsCount.WithLabelValues("size-error").Inc()
		return 0, fmt.Errorf("failed to get image status: %w", err)
	}

	if imageStatusResponse == nil {
		metrics.OperationErrorsCount.WithLabelValues("size-error").Inc()
		return 0, fmt.Errorf("image status response is nil")
	}

	if imageStatusResponse.Image == nil {
		metrics.OperationErrorsCount.WithLabelValues("size-error").Inc()
		return 0, fmt.Errorf("image info is nil in status response")
	}

	return imageStatusResponse.Image.Size(), nil
}

// Pull downloads the container image
func (p puller) Pull(ctx context.Context) (err error) {
	startTime := time.Now()

	// Setup deferred metrics collection
	defer func() {
		p.recordPullMetrics(startTime, err, ctx)
	}()

	// Create image spec for CRI API
	imageSpec := &cri.ImageSpec{Image: p.ImageWithTag()}

	// First try without credentials
	if err = p.pullWithoutCredentials(ctx, imageSpec); err == nil {
		return nil // Success without credentials
	}

	// If public pull failed, try with credentials
	return p.pullWithCredentials(ctx, imageSpec, err)
}

// recordPullMetrics records metrics about the image pull operation
func (p puller) recordPullMetrics(startTime time.Time, err error, ctx context.Context) {
	elapsed := time.Since(startTime).Seconds()
	imageTag := p.ImageWithTag()

	// Record pull time metrics
	klog.Infof("Pulled %s in %d milliseconds", imageTag, int(1000*elapsed))
	metrics.ImagePullTimeHist.WithLabelValues(metrics.BoolToString(err != nil)).Observe(elapsed)
	metrics.ImagePullTime.WithLabelValues(imageTag, metrics.BoolToString(err != nil)).Set(elapsed)

	// Record errors if any
	if err != nil {
		metrics.OperationErrorsCount.WithLabelValues("pull-error").Inc()
	}

	// Schedule cleanup of metrics after 1 minute
	go func() {
		time.Sleep(1 * time.Minute)
		metrics.ImagePullTime.DeleteLabelValues(imageTag, metrics.BoolToString(err != nil))
	}()

	// Record size metrics if pull was successful
	if err == nil {
		p.recordSizeMetrics(ctx, imageTag)
	}
}

// recordSizeMetrics records metrics about the image size
func (p puller) recordSizeMetrics(ctx context.Context, imageTag string) {
	size, err := p.ImageSize(ctx)
	if err != nil {
		return // Error already logged in ImageSize()
	}

	klog.Infof("Pulled %s with size of %d bytes", imageTag, size)
	metrics.ImagePullSizeBytes.WithLabelValues(imageTag).Set(float64(size))

	// Schedule cleanup of metrics after 1 minute
	go func() {
		time.Sleep(1 * time.Minute)
		metrics.ImagePullSizeBytes.DeleteLabelValues(imageTag)
	}()
}

// pullWithoutCredentials attempts to pull the image without authentication
func (p puller) pullWithoutCredentials(ctx context.Context, imageSpec *cri.ImageSpec) error {
	klog.V(2).Infof("Attempting to pull image %s without credentials", p.ImageWithTag())

	_, err := p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
		Image: imageSpec,
	})

	if err == nil {
		klog.V(2).Infof("Successfully pulled image %s without credentials", p.ImageWithTag())
		return nil
	}

	klog.V(2).Infof("Pull without credentials failed for %s: %v", p.ImageWithTag(), err)
	return err
}

// pullWithCredentials attempts to pull the image using credentials from the keyring
func (p puller) pullWithCredentials(ctx context.Context, imageSpec *cri.ImageSpec, initialErr error) error {
	// Look up credentials for this image repository
	repo := p.ImageWithoutTag()
	authConfigs, withCredentials := p.keyring.Lookup(repo)

	// If no credentials are available, return the original error
	if !withCredentials || len(authConfigs) == 0 {
		klog.V(2).Infof("No credentials found for %s", p.ImageWithTag())
		return fmt.Errorf("failed to pull image without credentials and no credentials available: %w", initialErr)
	}

	klog.V(2).Infof("Found %d credential options for image %s", len(authConfigs), p.ImageWithTag())

	// Try each credential option
	return p.tryCredentials(ctx, imageSpec, authConfigs)
}

// tryCredentials attempts to pull the image with each credential option
func (p puller) tryCredentials(ctx context.Context, imageSpec *cri.ImageSpec, authConfigs []*cri.AuthConfig) error {
	var pullErrs []error

	// Try each credential until one succeeds
	for i, authConfig := range authConfigs {
		klog.V(2).Infof("Trying credential option %d for image %s", i+1, p.ImageWithTag())

		// Try pulling with this credential
		if err := p.pullWithAuth(ctx, imageSpec, authConfig, i+1); err == nil {
			return nil // Success
		} else {
			pullErrs = append(pullErrs, err)
		}
	}

	// All credential options failed
	err := utilerrors.NewAggregate(pullErrs)
	klog.Warningf("All %d credential options failed for image %s",
		len(authConfigs), p.ImageWithTag())
	return err
}

// pullWithAuth attempts to pull using a specific credential
func (p puller) pullWithAuth(ctx context.Context, imageSpec *cri.ImageSpec, auth *cri.AuthConfig, optionNum int) error {
	_, err := p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
		Image: imageSpec,
		Auth:  auth,
	})

	if err == nil {
		klog.Infof("Successfully pulled image %s with credential option %d", p.ImageWithTag(), optionNum)
		return nil
	}

	klog.V(2).Infof("Pull with credential option %d failed: %v", optionNum, err)
	return fmt.Errorf("auth option %d: %w", optionNum, err)
}
