package remoteimage

import (
	"context"
	"time"

	"github.com/containerd/containerd/reference/docker"
	"github.com/warm-metal/container-image-csi-driver/pkg/metrics"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

type Puller interface {
	Pull(context.Context) error
	ImageSize(context.Context) int
}

func NewPuller(imageSvc cri.ImageServiceClient, image docker.Named,
	keyring credentialprovider.DockerKeyring) Puller {
	return &puller{
		imageSvc: imageSvc,
		image:    image,
		keyring:  keyring,
	}
}

type puller struct {
	imageSvc cri.ImageServiceClient
	image    docker.Named
	keyring  credentialprovider.DockerKeyring
}

// Returns the compressed size of the image that was pulled in bytes
// see https://github.com/containerd/containerd/issues/9261
func (p puller) ImageSize(ctx context.Context) int {
	imageSpec := &cri.ImageSpec{Image: p.image.String()}
	imageStatusResponse, _ := p.imageSvc.ImageStatus(ctx, &cri.ImageStatusRequest{
		Image: imageSpec,
	})
	return int(imageStatusResponse.Image.Size_)
}

func (p puller) Pull(ctx context.Context) (err error) {
	startTime := time.Now()
	defer func() { // must capture final value of "err"
		elapsed := time.Since(startTime).Seconds()
		metrics.ImagePullTimeHist.WithLabelValues(metrics.BoolToString(err != nil)).Observe(elapsed)
		metrics.ImagePullTime.WithLabelValues(p.image.String(), metrics.BoolToString(err != nil)).Set(elapsed)
		go func() {
			//TODO: this is a hack to ensure data is cleared in a reasonable timeframe and does not build up.
			// pushgateway may remove the need for this. https://prometheus.io/docs/practices/pushing/
			time.Sleep(1 * time.Minute)
			metrics.ImagePullTime.DeleteLabelValues(p.image.String(), metrics.BoolToString(err != nil))
		}()
		if err != nil {
			metrics.OperationErrorsCount.WithLabelValues("pull-error").Inc()
		}
	}()
	repo := p.image.Name()
	imageSpec := &cri.ImageSpec{Image: p.image.String()}
	creds, withCredentials := p.keyring.Lookup(repo)
	klog.V(2).Infof("remoteimage.Pull(): len(creds)=%d, withCreds=%t", len(creds), withCredentials)
	if !withCredentials {
		_, err = p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
			Image: imageSpec,
		})

		klog.V(2).Infof("remoteimage.Pull(no creds): completed with err=%v", err)
		return
	}

	var pullErrs []error
	for _, cred := range creds {
		auth := &cri.AuthConfig{
			Username:      cred.Username,
			Password:      cred.Password,
			Auth:          cred.Auth,
			ServerAddress: cred.ServerAddress,
			IdentityToken: cred.IdentityToken,
			RegistryToken: cred.RegistryToken,
		}

		_, err = p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
			Image: imageSpec,
			Auth:  auth,
		})

		if err == nil {
			klog.V(2).Info("remoteimage.Pull(with creds): completed with err==nil")
			return
		}

		pullErrs = append(pullErrs, err)
	}

	err = utilerrors.NewAggregate(pullErrs)
	klog.V(2).Infof("remoteimage.Pull(): completed with errors, len(pullErrs)=%d, aggErr=%s", len(pullErrs), err.Error())
	return
}
