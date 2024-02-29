package remoteimage

import (
	"context"

	"github.com/containerd/containerd/reference/docker"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
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
	repo := p.image.Name()
	imageSpec := &cri.ImageSpec{Image: p.image.String()}
	creds, withCredentials := p.keyring.Lookup(repo)
	if !withCredentials {
		_, err = p.imageSvc.PullImage(ctx, &cri.PullImageRequest{
			Image: imageSpec,
		})

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
			return
		}

		pullErrs = append(pullErrs, err)
	}

	return utilerrors.NewAggregate(pullErrs)
}
