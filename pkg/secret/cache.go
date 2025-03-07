package secret

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// DockerKeyring interface for registry authentication
type DockerKeyring interface {
	Lookup(image string) ([]authn.AuthConfig, bool)
}

// BasicDockerKeyring implements DockerKeyring using go-containerregistry's keychain
type BasicDockerKeyring struct {
	keychain authn.Keychain
	configs  []map[string]authn.AuthConfig
}

// Store interface for managing Docker credentials
type Store interface {
	GetDockerKeyring(ctx context.Context, secrets map[string]string) (DockerKeyring, error)
}

func makeDockerKeyringFromSecrets(secrets []corev1.Secret) (DockerKeyring, error) {
	keyring := &BasicDockerKeyring{
		keychain: authn.DefaultKeychain,
	}

	for _, secret := range secrets {
		if len(secret.Data) == 0 {
			continue
		}
		config, err := parseDockerConfigFromSecretData(byteSecretData(secret.Data))
		if err != nil {
			klog.Errorf("unable to parse secret: %v, data: %#v", err, secret)
			continue // Skip invalid secrets instead of failing
		}
		keyring.configs = append(keyring.configs, config)
	}

	return keyring, nil
}

func makeDockerKeyringFromMap(secretData map[string]string) (DockerKeyring, error) {
	keyring := &BasicDockerKeyring{
		keychain: authn.DefaultKeychain,
	}

	if len(secretData) > 0 {
		config, err := parseDockerConfigFromSecretData(stringSecretData(secretData))
		if err != nil {
			return nil, err
		}
		keyring.configs = append(keyring.configs, config)
	}

	return keyring, nil
}

type secretDataWrapper interface {
	Get(key string) (data []byte, existed bool)
}

type byteSecretData map[string][]byte

func (b byteSecretData) Get(key string) (data []byte, existed bool) {
	data, existed = b[key]
	return
}

type stringSecretData map[string]string

func (s stringSecretData) Get(key string) (data []byte, existed bool) {
	str, existed := s[key]
	if existed {
		data = []byte(str)
	}
	return
}

func parseDockerConfigFromSecretData(data secretDataWrapper) (map[string]authn.AuthConfig, error) {
	dockerConfigKey := ""
	if _, ok := data.Get(corev1.DockerConfigJsonKey); ok {
		dockerConfigKey = corev1.DockerConfigJsonKey
	} else if _, ok := data.Get(corev1.DockerConfigKey); ok {
		dockerConfigKey = corev1.DockerConfigKey
	} else {
		return map[string]authn.AuthConfig{}, nil
	}

	dockercfg, ok := data.Get(dockerConfigKey)
	if !ok {
		return map[string]authn.AuthConfig{}, nil
	}

	var cfg map[string]authn.AuthConfig
	if dockerConfigKey == corev1.DockerConfigJsonKey {
		var cfgV1 struct {
			Auths map[string]authn.AuthConfig `json:"auths"`
		}
		if err := json.Unmarshal(dockercfg, &cfgV1); err == nil {
			cfg = cfgV1.Auths
		} else {
			if err := json.Unmarshal(dockercfg, &cfg); err != nil {
				return nil, err
			}
		}
	} else {
		if err := json.Unmarshal(dockercfg, &cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// Lookup implements DockerKeyring interface
func (k *BasicDockerKeyring) Lookup(image string) ([]authn.AuthConfig, bool) {
	// First try go-containerregistry keychain
	ref, err := name.ParseReference(image)
	if err == nil {
		authenticator, err := k.keychain.Resolve(ref.Context())
		if err == nil && authenticator != authn.Anonymous {
			auth, err := authenticator.Authorization()
			if err == nil {
				return []authn.AuthConfig{*auth}, true
			}
		}
	}

	// Fallback to configs from secrets
	var matches []authn.AuthConfig
	for _, config := range k.configs {
		for registry, auth := range config {
			if matchImage(registry, image) {
				matches = append(matches, auth)
			}
		}
	}
	return matches, len(matches) > 0
}

// matchImage checks if an image matches a registry pattern
func matchImage(pattern, image string) bool {
	// Exact match
	if pattern == image {
		return true
	}

	// If pattern ends with /, it should match the registry/repository prefix
	if len(pattern) < len(image) && pattern[len(pattern)-1] == '/' {
		return image[:len(pattern)] == pattern
	}

	// Handle cases where the pattern is just the registry (e.g., private-registry:5000)
	if i := len(pattern); i < len(image) && image[i] == '/' {
		return image[:i] == pattern
	}

	return false
}

// UnionDockerKeyring allows merging multiple keyrings
type UnionDockerKeyring []DockerKeyring

// Lookup implements DockerKeyring interface for UnionDockerKeyring
func (uk UnionDockerKeyring) Lookup(image string) ([]authn.AuthConfig, bool) {
	for _, keyring := range uk {
		if auth, ok := keyring.Lookup(image); ok {
			return auth, true
		}
	}
	return nil, false
}

// NewEmptyKeyring returns an empty credential keyring
func NewEmptyKeyring() DockerKeyring {
	return &BasicDockerKeyring{
		keychain: authn.DefaultKeychain,
	}
}

type persistentKeyringGetter interface {
	Get(context.Context) DockerKeyring
}

type keyringStore struct {
	persistentKeyringGetter
}

func (s keyringStore) GetDockerKeyring(ctx context.Context, secretData map[string]string) (keyring DockerKeyring, err error) {
	var preferredKeyring DockerKeyring
	if len(secretData) > 0 {
		preferredKeyring, err = makeDockerKeyringFromMap(secretData)
		if err != nil {
			return nil, err
		}
	}

	daemonKeyring := s.Get(ctx)
	if preferredKeyring != nil {
		return UnionDockerKeyring{preferredKeyring, daemonKeyring}, nil
	}

	return UnionDockerKeyring{daemonKeyring, NewEmptyKeyring()}, err
}

type secretFetcher struct {
	Client       *kubernetes.Clientset
	nodePluginSA string
	Namespace    string
}

func (f secretFetcher) Fetch(ctx context.Context) ([]corev1.Secret, error) {
	sa, err := f.Client.CoreV1().ServiceAccounts(f.Namespace).Get(ctx, f.nodePluginSA, metav1.GetOptions{})
	if err != nil {
		klog.Errorf(`unable to fetch service account of the daemon pod "%s/%s": %s`, f.Namespace, f.nodePluginSA, err)
		return nil, err
	}

	secrets := make([]corev1.Secret, 0, len(sa.ImagePullSecrets))
	klog.V(2).Infof(
		`got %d imagePullSecrets from the service account %s/%s`, len(sa.ImagePullSecrets), f.Namespace, f.nodePluginSA,
	)

	for i := range sa.ImagePullSecrets {
		s := &sa.ImagePullSecrets[i]
		secret, err := f.Client.CoreV1().Secrets(f.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf(`unable to fetch secret "%s/%s": %s`, f.Namespace, s.Name, err)
			continue
		}

		secrets = append(secrets, *secret)
	}

	return secrets, nil
}

func (s secretFetcher) Get(ctx context.Context) DockerKeyring {
	secrets, _ := s.Fetch(ctx)
	keyring, _ := makeDockerKeyringFromSecrets(secrets)
	return keyring
}

func createSecretFetcher(nodePluginSA string) *secretFetcher {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("unable to get cluster config: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("unable to get cluster client: %s", err)
	}

	curNamespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		klog.Fatalf("unable to fetch the current namespace from the sa volume: %q", err.Error())
	}

	return &secretFetcher{
		Client:       clientset,
		nodePluginSA: nodePluginSA,
		Namespace:    string(curNamespace),
	}
}

func createFetcherOrDie(nodePluginSA string) Store {
	return keyringStore{
		persistentKeyringGetter: createSecretFetcher(nodePluginSA),
	}
}

type secretWOCache struct {
	daemonKeyring DockerKeyring
}

func (s secretWOCache) Get(_ context.Context) DockerKeyring {
	return s.daemonKeyring
}

func createCacheOrDie(nodePluginSA string) Store {
	secretFetcher := createSecretFetcher(nodePluginSA)
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	var keyring DockerKeyring
	secrets, _ := secretFetcher.Fetch(ctx)
	keyring, _ = makeDockerKeyringFromSecrets(secrets)
	return keyringStore{
		persistentKeyringGetter: secretWOCache{
			daemonKeyring: keyring,
		},
	}
}

func CreateStoreOrDie(pluginConfigFile, pluginBinDir, nodePluginSA string, enableCache bool) Store {
	if len(pluginConfigFile) > 0 && len(pluginBinDir) > 0 {
		// The k8s.io/kubernetes/pkg/credentialprovider API is different
		// We'll use the built-in keyring for now since plugin support changed
		// credentialprovider.SetPreferredDockercfgPath(pluginConfigFile)
	}

	if enableCache {
		return createCacheOrDie(nodePluginSA)
	}

	return createFetcherOrDie(nodePluginSA)
}
