package remoteimage

import (
	"context"
	"sync"

	"github.com/containerd/containerd/reference/docker"
	"golang.org/x/sync/semaphore"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

type CurrentPull struct {
	semaphore *semaphore.Weighted
	err       error
}

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
	imageSvc     cri.ImageServiceClient
	image        docker.Named
	keyring      credentialprovider.DockerKeyring
	lock         *sync.Mutex
	parallelPull map[string]*CurrentPull
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

	p.lock.Lock()
	if p.parallelPull == nil {
		p.parallelPull = make(map[string]*CurrentPull)
	}

	currentPullLock, ok := p.parallelPull[repo]
	if !ok {
		p.parallelPull[repo] = &CurrentPull{
			semaphore: semaphore.NewWeighted(1),
		}
		currentPullLock = p.parallelPull[repo]
	}
	p.lock.Unlock()

	doingPull := currentPullLock.semaphore.TryAcquire(1)
	if !doingPull {
		klog.Info("Pulling of image %s is already in progress wait until completed", repo)
		currentPullLock.semaphore.Acquire(ctx, 1)
	} else {
		klog.Info("Pulling of image %s not in progress, starting now", repo)
	}

	defer currentPullLock.semaphore.Release(1)

	if !doingPull {
		klog.Info("Pulling of image %s is completed", repo)
		return currentPullLock.err
	}

	defer func() {
		p.lock.Lock()
		delete(p.parallelPull, repo)
		p.lock.Unlock()
	}()

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
			klog.Info("Image pull completed successfully")
			return
		}

		pullErrs = append(pullErrs, err)
	}

	currentPullLock.err = utilerrors.NewAggregate(pullErrs)
	return err
}
