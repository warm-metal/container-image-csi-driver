package imagesize

import (
	"context"
	"fmt"
	"testing"

	manifesttypes "github.com/docker/cli/cli/manifest/types"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

func TestWarn(t *testing.T) {
	fakeClient := &fake.Clientset{}

	fakeRecorder := record.NewFakeRecorder(1)
	maxSize := resource.MustParse("2Gi")
	Warner = &warner{
		recorder:     fakeRecorder,
		clientSet:    fakeClient,
		maxImageSize: &maxSize,
	}

	fakeClient.Fake.AddReactor("get", "pods", func(action core.Action) (bool, runtime.Object, error) {
		getAction := action.(core.GetAction)

		if getAction == nil {
			return false, nil, nil
		}
		if getAction.GetName() == "test-pod" {
			return true, &apiv1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:       types.UID("test-pod"),
					Namespace: "default",
					Name:      "test-pod",
				}}, nil
		}

		return true, nil, errors.NewNotFound(apiv1.Resource("pod"), getAction.GetName())
	})

	t.Run("image size is too large", func(t *testing.T) {
		// image size is too large
		currentSize := resource.MustParse("3Gi")
		err := Warner.Warn(&currentSize, "test-pod", "default")
		assert.NoError(t, err)

		select {
		case event := <-fakeRecorder.Events:
			assert.Contains(t, event, "ImageSizeTooLarge")
		default:
			t.Fatal("Test case failed. 'ImageSizeTooLarge' event was expected")
		}
	})

	t.Run("image size is within bounds", func(t *testing.T) {
		// image size is within bounds
		currentSize := resource.MustParse("1Gi")
		err := Warner.Warn(&currentSize, "test-pod", "default")
		assert.NoError(t, err)

		select {
		case event := <-fakeRecorder.Events:
			t.Fatalf("Test case failed. 'ImageSizeTooLarge' event was not expected (got '%v')", event)
		default:
			klog.Info("no event was logged as expected")
		}
	})

	t.Run("image size is equal to bounds", func(t *testing.T) {
		// image size is equal to bounds
		currentSize := resource.MustParse("2Gi")
		err := Warner.Warn(&currentSize, "test-pod", "default")
		assert.NoError(t, err)

		select {
		case event := <-fakeRecorder.Events:
			t.Fatalf("Test case failed. 'ImageSizeTooLarge' event was not expected (got '%v')", event)
		default:
			klog.Info("no event was logged as expected")
		}
	})

	t.Run("failed to get pod info", func(t *testing.T) {
		// failed to get pod info
		currentSize := resource.MustParse("3Gi")
		err := Warner.Warn(&currentSize, "some-other-pod", "default")
		assert.ErrorContains(t, err, "failed to get pod info")

		select {
		case event := <-fakeRecorder.Events:
			t.Fatalf("Test case failed. 'ImageSizeTooLarge' event was not expected (got '%v')", event)
		default:
			klog.Info("no event was logged as expected")
		}
	})

}

func TestFetchImageSize(t *testing.T) {
	fakeClient := &fake.Clientset{}

	fakeRecorder := record.NewFakeRecorder(1)
	maxSize := resource.MustParse("2Gi")
	Warner = &warner{
		recorder:     fakeRecorder,
		clientSet:    fakeClient,
		maxImageSize: &maxSize,
	}

	t.Run("OCIManifest is used", func(t *testing.T) {
		c := &testRegistryClient{
			testImageManifests: []manifesttypes.ImageManifest{
				{
					Descriptor: v1.Descriptor{
						Platform: &v1.Platform{
							OS:           "linux",
							Architecture: "amd64",
						},
					},
					OCIManifest: &ocischema.DeserializedManifest{
						Manifest: ocischema.Manifest{
							Layers: []distribution.Descriptor{
								{
									Size: 9000,
								},
								{
									Size: 1000,
								},
							},
						},
					},
				},
			},
		}

		image, err := reference.ParseNamed("docker.io/library/nginx:latest")
		if err != nil {
			panic(err)
		}
		q, e := Warner.fetchImageSize(c, image)
		assert.NoError(t, e)
		assert.NotNil(t, q)
		assert.True(t, q.Cmp(resource.MustParse("10000")) == 0)
	})

	t.Run("SchemaV2 manifest is used", func(t *testing.T) {
		c := &testRegistryClient{
			testImageManifests: []manifesttypes.ImageManifest{
				{
					Descriptor: v1.Descriptor{
						Platform: &v1.Platform{
							OS:           "linux",
							Architecture: "amd64",
						},
					},

					SchemaV2Manifest: &schema2.DeserializedManifest{
						Manifest: schema2.Manifest{
							Layers: []distribution.Descriptor{
								{
									Size: 9000,
								},
								{
									Size: 1000,
								},
							},
						},
					},
				},
			},
		}

		image, err := reference.ParseNamed("docker.io/library/nginx:latest")
		if err != nil {
			panic(err)
		}
		q, e := Warner.fetchImageSize(c, image)
		assert.NoError(t, e)
		assert.NotNil(t, q)
		assert.True(t, q.Cmp(resource.MustParse("10000")) == 0)
	})

	t.Run("Both OCI and SchemaV2 manifests are present", func(t *testing.T) {
		c := &testRegistryClient{
			testImageManifests: []manifesttypes.ImageManifest{
				{
					Descriptor: v1.Descriptor{
						Platform: &v1.Platform{
							OS:           "linux",
							Architecture: "amd64",
						},
					},

					SchemaV2Manifest: &schema2.DeserializedManifest{
						Manifest: schema2.Manifest{
							Layers: []distribution.Descriptor{
								{
									Size: 9000,
								},
								{
									Size: 1000,
								},
							},
						},
					},
					OCIManifest: &ocischema.DeserializedManifest{
						Manifest: ocischema.Manifest{
							Layers: []distribution.Descriptor{
								{
									Size: 9000,
								},
								{
									Size: 1000,
								},
							},
						},
					},
				},
			},
		}

		image, err := reference.ParseNamed("docker.io/library/nginx:latest")
		if err != nil {
			panic(err)
		}
		q, e := Warner.fetchImageSize(c, image)
		assert.NoError(t, e)
		assert.NotNil(t, q)
		assert.True(t, q.Cmp(resource.MustParse("10000")) == 0)
	})

	t.Run("Both OCI and SchemaV2 manifests are nil", func(t *testing.T) {
		c := &testRegistryClient{
			testImageManifests: []manifesttypes.ImageManifest{
				{
					Descriptor: v1.Descriptor{
						Platform: &v1.Platform{
							OS:           "linux",
							Architecture: "amd64",
						},
					},
				},
			},
		}

		image, err := reference.ParseNamed("docker.io/library/nginx:latest")
		if err != nil {
			panic(err)
		}
		q, e := Warner.fetchImageSize(c, image)
		assert.Error(t, e)
		assert.Nil(t, q)
		assert.ErrorContains(t, e, "both OCI and Schema2 manifests are nil for the image")
	})

	t.Run("Both GetManifest and GetManifest list returns nothing", func(t *testing.T) {
		c := &testRegistryClient{}

		image, err := reference.ParseNamed("docker.io/library/nginx:latest")
		if err != nil {
			panic(err)
		}
		q, e := Warner.fetchImageSize(c, image)
		assert.Error(t, e)
		assert.Nil(t, q)
		assert.ErrorContains(t, e, "failed to get image manifest and manifest list")
	})

	t.Run("No linux/amd64 manifest present", func(t *testing.T) {
		c := &testRegistryClient{
			testImageManifests: []manifesttypes.ImageManifest{
				{
					Descriptor: v1.Descriptor{
						Platform: &v1.Platform{
							OS:           "darwin",
							Architecture: "arm64",
						},
					},

					SchemaV2Manifest: &schema2.DeserializedManifest{
						Manifest: schema2.Manifest{
							Layers: []distribution.Descriptor{
								{
									Size: 9000,
								},
								{
									Size: 1000,
								},
							},
						},
					},
					OCIManifest: &ocischema.DeserializedManifest{
						Manifest: ocischema.Manifest{
							Layers: []distribution.Descriptor{
								{
									Size: 9000,
								},
								{
									Size: 1000,
								},
							},
						},
					},
				},
			},
		}

		image, err := reference.ParseNamed("docker.io/library/nginx:latest")
		if err != nil {
			panic(err)
		}
		q, e := Warner.fetchImageSize(c, image)
		assert.Error(t, e)
		assert.Nil(t, q)
		assert.ErrorContains(t, e, "couldn't get image size for the image")
	})

}

type testRegistryClient struct {
	testImageManifests []manifesttypes.ImageManifest
}

func (t *testRegistryClient) GetManifest(ctx context.Context, ref reference.Named) (manifesttypes.ImageManifest, error) {
	if len(t.testImageManifests) == 0 {
		return manifesttypes.ImageManifest{}, fmt.Errorf("no manifest present")
	}
	return t.testImageManifests[0], nil
}
func (t *testRegistryClient) GetManifestList(ctx context.Context, ref reference.Named) ([]manifesttypes.ImageManifest, error) {
	if len(t.testImageManifests) == 0 {
		return t.testImageManifests, fmt.Errorf("no manifest present")
	}
	return t.testImageManifests, nil
}
func (t *testRegistryClient) MountBlob(ctx context.Context, source reference.Canonical, target reference.Named) error {
	return nil
}
func (t *testRegistryClient) PutManifest(ctx context.Context, ref reference.Named, manifest distribution.Manifest) (digest.Digest, error) {
	return "", nil
}
