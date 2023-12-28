package remoteimage

import (
	"context"

	"github.com/containerd/containerd/reference/docker"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

type Puller interface {
	Pull(context.Context) error
	ImageSize(context.Context) (int, error)
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

// Returns the size of the image that was pulled in MB(I think?) **TODO: check**
func (p puller) ImageSize(ctx context.Context) (int, error) {
	info, err := p.imageSvc.ImageFsInfo(ctx, &cri.ImageFsInfoRequest{})
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
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
