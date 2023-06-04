package watcher

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// ImageAnnotation is the annotation key for the image name.
	ImageAnnotation = "csi.storage.k8s.io/image"
)

// Watcher watches PVCs.
type Watcher struct {
	client      kubernetes.Interface
	pvcInformer cache.SharedIndexInformer
	pvcIndexer  cache.Indexer
	stopChan    chan struct{}
}

// New creates a new Watcher.
func New(ctx context.Context, resyncPeriod time.Duration) (*Watcher, error) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		if errors.Is(err, rest.ErrNotInCluster) {
			kubeConfig, err = clientcmd.BuildConfigFromFlags(
				"",
				clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename(),
			)
			if err != nil {
				return nil, err
			}
		}

		return nil, err
	}

	clientSet, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return clientSet.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return clientSet.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).Watch(ctx, options)
		},
	}

	indexers := cache.Indexers{
		"uid": func(obj interface{}) ([]string, error) {
			object, err := meta.Accessor(obj)
			if err != nil {
				return nil, err
			}

			return []string{string(object.GetUID())}, nil
		},
	}

	pvcInformer := cache.NewSharedIndexInformer(
		lw,
		&corev1.PersistentVolumeClaim{},
		resyncPeriod,
		indexers,
	)

	pvcIndexer := pvcInformer.GetIndexer()
	stopChan := make(chan struct{})

	go pvcInformer.Run(stopChan)

	return &Watcher{
		client:      clientSet,
		pvcInformer: pvcInformer,
		pvcIndexer:  pvcIndexer,
		stopChan:    stopChan,
	}, err
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.stopChan)
}

// GetImage returns the image name for the given PVC.
func (w *Watcher) GetImage(name string) (string, error) {
	pvc, err := w.getPVCFromIndexer(name)
	if err != nil {
		return "", err
	}

	if volumeHandle, ok := pvc.Annotations[ImageAnnotation]; ok {
		return volumeHandle, nil
	}

	return "", fmt.Errorf("pvc %s does not have volume handle annotation %s", name, ImageAnnotation)
}

func (w *Watcher) getPVCFromIndexer(name string) (*corev1.PersistentVolumeClaim, error) {
	uid := name[4:]

	pvc, err := w.pvcIndexer.ByIndex("uid", uid)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get pvc from indexer by uid %s", uid)
	} else if len(pvc) == 0 {
		return nil, fmt.Errorf("pvc with uid %s not found in indexer", uid)
	}

	return pvc[0].(*corev1.PersistentVolumeClaim), nil
}
