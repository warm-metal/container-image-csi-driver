package remoteimage

import (
	"context"
	"github.com/containerd/containerd/reference/docker"
	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
	credential "k8s.io/kubernetes/pkg/credentialprovider/secrets"
	"k8s.io/kubernetes/pkg/util/parsers"
)

type Puller interface {
	Pull(context.Context) error
}

func NewPuller(imageSvc cri.ImageServiceClient, image string, secrets []corev1.Secret) Puller {
	return &puller{
		imageSvc: imageSvc,
		image:    image,
		secrets:  secrets,
	}
}

type puller struct {
	imageSvc cri.ImageServiceClient
	image    string
	secrets  []corev1.Secret
}

func (p puller) Pull(ctx context.Context) (err error) {
	namedRef, err := docker.ParseDockerRef(p.image)
	if err != nil {
		klog.Errorf("fail to normalize image: %s, %s", p.image, err)
		return
	}

	keyRing, err := credential.MakeDockerKeyring(p.secrets, credentialprovider.NewDockerKeyring())
	if err != nil {
		klog.Errorf("keyring: %s", err)
		return
	}

	repo, _, _, err := parsers.ParseImageName(namedRef.String())
	if err != nil {
		klog.Errorf(`fail to parse "%s": %s`, namedRef, err)
		return
	}

	imageSpec := &cri.ImageSpec{Image: namedRef.String()}
	creds, withCredentials := keyRing.Lookup(repo)
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
