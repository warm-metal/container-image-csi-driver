package remoteimage

import (
	"context"
	"github.com/containerd/containerd/reference/docker"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	"k8s.io/kubernetes/pkg/credentialprovider"
	credential "k8s.io/kubernetes/pkg/credentialprovider/secrets"
	"k8s.io/kubernetes/pkg/util/parsers"
)

type Puller interface {
	Pull(context.Context) error
}

func NewPuller(imageSvc cri.ImageServiceClient, image, secret, secretNamespace, sa string) Puller {
	return &puller{
		imageSvc:        imageSvc,
		image:           image,
		secret:          secret,
		secretNamespace: secretNamespace,
		serviceAccount:  sa,
	}
}

type puller struct {
	imageSvc        cri.ImageServiceClient
	image           string
	secret          string
	secretNamespace string
	serviceAccount  string // FIXME DO NOT USE IT NOW
}

func (p puller) Pull(ctx context.Context) (err error) {
	namedRef, err := docker.ParseDockerRef(p.image)
	if err != nil {
		glog.Errorf("fail to normalize image: %s, %s", p.image, err)
		return
	}

	var secrets []v1.Secret
	if len(p.secret) > 0 {
		config, err := rest.InClusterConfig()
		if err != nil {
			glog.Errorf("can't get cluster config: %s", err)
			return err
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			glog.Errorf("can't get cluster client: %s", err)
			return err
		}

		secret, err := clientset.CoreV1().Secrets(p.secretNamespace).Get(ctx, p.secret, metav1.GetOptions{})
		if err != nil {
			glog.Errorf(`can't get secret "%s/%s": %s`, p.secretNamespace, p.secret, err)
			return err
		}

		secrets = append(secrets, *secret)
	}

	keyRing, err := credential.MakeDockerKeyring(secrets, credentialprovider.NewDockerKeyring())
	if err != nil {
		glog.Errorf("keyring: %s", err)
		return
	}

	repo, _, _, err := parsers.ParseImageName(namedRef.String())
	if err != nil {
		glog.Errorf(`fail to parse "%s": %s`, namedRef, err)
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
	}

	return utilerrors.NewAggregate(pullErrs)
}
