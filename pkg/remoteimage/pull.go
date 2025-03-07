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

type Puller interface {
	Pull(context.Context) error
	ImageWithTag() string
	ImageWithoutTag() string
	ImageSize(context.Context) (int, error)
}

func NewPuller(imageSvc cri.ImageServiceClient, image reference.Named,
	keyring secret.DockerKeyring) Puller {
	return &puller{
		imageSvc: imageSvc,
		image:    image,
		keyring:  keyring,
	}
}

type puller struct {
	imageSvc cri.ImageServiceClient
	image    reference.Named
	keyring  secret.DockerKeyring
}

func (p puller) ImageWithTag() string {
	return p.image.String()
}

func (p puller) ImageWithoutTag() string {
	return p.image.Name()
}

// Returns the compressed size of the image that was pulled in bytes
// see https://github.com/containerd/containerd/issues/9261
func (p puller) ImageSize(ctx context.Context) (size int, err error) {
	defer func() {
		if err != nil {
			klog.Errorf("%s", err.Error())
			metrics.OperationErrorsCount.WithLabelValues("size-error").Inc()
		}
	}()
	imageSpec := &cri.ImageSpec{Image: p.ImageWithTag()}
	if imageStatusResponse, err := p.imageSvc.ImageStatus(ctx, &cri.ImageStatusRequest{
		Image: imageSpec,
	}); err != nil {
		size = 0
		err = fmt.Errorf("remoteimage.ImageSize(): call returned an error: %s", err.Error())
		return size, err
	} else if imageStatusResponse == nil {
		size = 0
		err = fmt.Errorf("remoteimage.ImageSize(): imageStatusResponse is nil")
		return size, err
	} else if imageStatusResponse.Image == nil {
		size = 0
		err = fmt.Errorf("remoteimage.ImageSize(): imageStatusResponse.Image is nil")
		return size, err
	} else {
		size = imageStatusResponse.Image.Size()
		err = nil
		return size, err
	}
}

func (p puller) Pull(ctx context.Context) (err error) {
	startTime := time.Now()
	defer func() { // must capture final value of "err"
		elapsed := time.Since(startTime).Seconds()
		// pull time metrics and logs
		klog.Infof("remoteimage.Pull(): pulled %s in %d milliseconds", p.ImageWithTag(), int(1000*elapsed))
		metrics.ImagePullTimeHist.WithLabelValues(metrics.BoolToString(err != nil)).Observe(elapsed)
		metrics.ImagePullTime.WithLabelValues(p.ImageWithTag(), metrics.BoolToString(err != nil)).Set(elapsed)
		if err != nil {
			metrics.OperationErrorsCount.WithLabelValues("pull-error").Inc()
		}
		go func() {
			//TODO: this is a hack to ensure data is cleared in a reasonable time frame (after scrape) and does not build up.
			time.Sleep(1 * time.Minute)
			metrics.ImagePullTime.DeleteLabelValues(p.ImageWithTag(), metrics.BoolToString(err != nil))
		}()
		// pull size metrics and logs
		if err == nil { // only size if pull was successful
			if size, err2 := p.ImageSize(ctx); err2 != nil {
				// log entries and error counts emitted inside ImageSize() method
			} else { // success
				klog.Infof("remoteimage.Pull(): pulled %s with size of %d bytes", p.ImageWithTag(), size)
				metrics.ImagePullSizeBytes.WithLabelValues(p.ImageWithTag()).Set(float64(size))
				go func() {
					//TODO: this is a hack to ensure data is cleared in a reasonable time frame (after scrape) and does not build up.
					time.Sleep(1 * time.Minute)
					metrics.ImagePullSizeBytes.DeleteLabelValues(p.ImageWithTag())
				}()
			}
		}
	}()

	repo := p.ImageWithoutTag()
	imageSpec := &cri.ImageSpec{Image: p.ImageWithTag()}
	authConfigs, withCredentials := p.keyring.Lookup(repo)
	if !withCredentials {
		_, err = p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
			Image: imageSpec,
		})
		klog.V(2).Infof("remoteimage.Pull(no creds): pulling %s completed with err=%v", p.ImageWithTag(), err)
		return
	}

	var pullErrs []error
	for _, authConfig := range authConfigs {
		auth := &cri.AuthConfig{
			Username:      authConfig.Username,
			Password:      authConfig.Password,
			Auth:          authConfig.Auth,
			ServerAddress: authConfig.RegistryToken, // Using RegistryToken as ServerAddress since authConfig doesn't have a dedicated field
			IdentityToken: authConfig.Password,      // Using Password as IdentityToken if using token auth
			RegistryToken: authConfig.RegistryToken,
		}

		_, err = p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
			Image: imageSpec,
			Auth:  auth,
		})

		if err == nil {
			klog.V(2).Infof("remoteimage.Pull(with creds): pulling %s completed with err==nil", p.ImageWithTag())
			return
		}
		pullErrs = append(pullErrs, err)
	}

	err = utilerrors.NewAggregate(pullErrs)
	klog.V(2).Infof("remoteimage.Pull(): completed with errors, len(pullErrs)=%d, aggErr=%s", len(pullErrs), err.Error())
	return
}
