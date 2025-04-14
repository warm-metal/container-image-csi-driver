package secret

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// Store is an interface for retrieving Docker credentials.
type Store interface {
	GetDockerKeyring(ctx context.Context, secrets map[string]string) (DockerKeyring, error)
}

// secretDataWrapper abstracts data access for both byte slices and strings
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
	strings, existed := s[key]
	if existed {
		data = []byte(strings)
	}
	return
}

// parseDockerConfigFromSecretData extracts Docker config from secret data
func parseDockerConfigFromSecretData(data secretDataWrapper) (DockerConfig, error) {
	// First check for the newer .dockerconfigjson format
	if dockerConfigJSONBytes, existed := data.Get(corev1.DockerConfigJsonKey); existed && len(dockerConfigJSONBytes) > 0 {
		return parseDockerConfigJSON(dockerConfigJSONBytes)
	}

	// Then check for the legacy .dockercfg format
	if dockercfgBytes, existed := data.Get(corev1.DockerConfigKey); existed && len(dockercfgBytes) > 0 {
		return parseLegacyDockerConfig(dockercfgBytes)
	}

	return nil, nil
}

// parseDockerConfigJSON parses the newer .dockerconfigjson format
func parseDockerConfigJSON(data []byte) (DockerConfig, error) {
	dockerConfigJSON := DockerConfigJSON{}
	if err := json.Unmarshal(data, &dockerConfigJSON); err != nil {
		return nil, fmt.Errorf("error parsing .dockerconfigjson: %w", err)
	}
	return dockerConfigJSON.Auths, nil
}

// parseLegacyDockerConfig parses the legacy .dockercfg format
func parseLegacyDockerConfig(data []byte) (DockerConfig, error) {
	dockercfg := DockerConfig{}
	if err := json.Unmarshal(data, &dockercfg); err != nil {
		return nil, fmt.Errorf("error parsing .dockercfg: %w", err)
	}
	return dockercfg, nil
}

// makeDockerKeyringFromSecrets creates a keyring from a list of Kubernetes secrets
func makeDockerKeyringFromSecrets(secrets []corev1.Secret) (DockerKeyring, error) {
	keyring := &BasicDockerKeyring{}

	for _, secret := range secrets {
		if len(secret.Data) == 0 {
			continue
		}

		cred, err := parseDockerConfigFromSecretData(byteSecretData(secret.Data))
		if err != nil {
			klog.Errorf(`unable to parse secret %s/%s: %v`, secret.Namespace, secret.Name, err)
			return nil, err
		}

		if cred != nil {
			keyring.Add(cred)
		}
	}

	return keyring, nil
}

// makeDockerKeyringFromMap creates a keyring from a map of strings (from CSI volume context)
func makeDockerKeyringFromMap(secretData map[string]string) (DockerKeyring, error) {
	keyring := &BasicDockerKeyring{}

	if len(secretData) > 0 {
		cred, err := parseDockerConfigFromSecretData(stringSecretData(secretData))
		if err != nil {
			klog.Errorf(`unable to parse secret data: %v`, err)
			return nil, err
		}

		if cred != nil {
			keyring.Add(cred)
		}
	}

	return keyring, nil
}

// keyringProvider is an interface for anything that can provide a DockerKeyring
type keyringProvider interface {
	GetKeyring(ctx context.Context) (DockerKeyring, error)
}

// credentialStore is a unified credential store implementation
type credentialStore struct {
	secretsFetcher keyringProvider
	pluginsEnabled bool
}

// GetDockerKeyring returns credentials from volume context, Kubernetes secrets, and plugins
func (s credentialStore) GetDockerKeyring(ctx context.Context, secretData map[string]string) (DockerKeyring, error) {
	keyrings := s.collectKeyrings(ctx, secretData)

	return s.createUnionKeyring(keyrings), nil
}

// collectKeyrings gathers credentials from all available sources in priority order
func (s credentialStore) collectKeyrings(ctx context.Context, secretData map[string]string) []DockerKeyring {
	var keyrings []DockerKeyring

	// First check volume context (highest priority)
	if len(secretData) > 0 {
		volumeKeyring, err := makeDockerKeyringFromMap(secretData)
		if err == nil && volumeKeyring != nil {
			keyrings = append(keyrings, volumeKeyring)
			klog.V(3).Info("Added volume context credentials to keyring")
		}
	}

	// Next check Kubernetes secrets
	if s.secretsFetcher != nil {
		secretKeyring, err := s.secretsFetcher.GetKeyring(ctx)
		if err == nil && secretKeyring != nil {
			keyrings = append(keyrings, secretKeyring)
			klog.V(3).Info("Added Kubernetes secret credentials to keyring")
		}
	}

	// Finally add plugin-based keyring if enabled
	if s.pluginsEnabled {
		keyrings = append(keyrings, &pluginDockerKeyring{})
		klog.V(3).Info("Added plugin credentials to keyring")
	}

	return keyrings
}

// createUnionKeyring combines multiple keyrings into a single keyring interface
func (s credentialStore) createUnionKeyring(keyrings []DockerKeyring) DockerKeyring {
	switch len(keyrings) {
	case 0:
		return NewDockerKeyring()
	case 1:
		return keyrings[0]
	default:
		return UnionDockerKeyring(keyrings)
	}
}

// secretFetcher fetches Kubernetes secrets for authentication
type secretFetcher struct {
	Client       *kubernetes.Clientset
	nodePluginSA string
	Namespace    string
}

// Fetch gets secrets from the service account
func (f secretFetcher) Fetch(ctx context.Context) ([]corev1.Secret, error) {
	sa, err := f.getServiceAccount(ctx)
	if err != nil {
		return nil, err
	}

	klog.V(2).Infof(
		`Found %d imagePullSecrets in service account %s/%s`,
		len(sa.ImagePullSecrets), f.Namespace, f.nodePluginSA,
	)

	return f.getSecrets(ctx, sa.ImagePullSecrets)
}

// getServiceAccount retrieves the ServiceAccount specified in the configuration
func (f secretFetcher) getServiceAccount(ctx context.Context) (*corev1.ServiceAccount, error) {
	sa, err := f.Client.CoreV1().ServiceAccounts(f.Namespace).Get(ctx, f.nodePluginSA, metav1.GetOptions{})
	if err != nil {
		klog.Errorf(`Unable to fetch service account "%s/%s": %s`, f.Namespace, f.nodePluginSA, err)
		return nil, fmt.Errorf("failed to get service account %s/%s: %w", f.Namespace, f.nodePluginSA, err)
	}
	return sa, nil
}

// getSecrets retrieves all the secrets referenced by the service account
func (f secretFetcher) getSecrets(ctx context.Context, secretRefs []corev1.LocalObjectReference) ([]corev1.Secret, error) {
	secrets := make([]corev1.Secret, 0, len(secretRefs))

	for _, ref := range secretRefs {
		secret, err := f.Client.CoreV1().Secrets(f.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf(`Unable to fetch secret "%s/%s": %s`, f.Namespace, ref.Name, err)
			continue
		}

		secrets = append(secrets, *secret)
	}

	return secrets, nil
}

// GetKeyring gets a keyring from Kubernetes secrets
func (f secretFetcher) GetKeyring(ctx context.Context) (DockerKeyring, error) {
	secrets, err := f.Fetch(ctx)
	if err != nil {
		return NewDockerKeyring(), err
	}

	keyring, err := makeDockerKeyringFromSecrets(secrets)
	if err != nil {
		return NewDockerKeyring(), err
	}

	return keyring, nil
}

// createSecretFetcher creates a new secretFetcher for Kubernetes secrets
func createSecretFetcher(nodePluginSA string) (*secretFetcher, error) {
	config, err := getKubernetesConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("unable to create Kubernetes client: %w", err)
	}

	namespace, err := getCurrentNamespace()
	if err != nil {
		return nil, err
	}

	return &secretFetcher{
		Client:       clientset,
		nodePluginSA: nodePluginSA,
		Namespace:    namespace,
	}, nil
}

// getKubernetesConfig creates a Kubernetes client configuration
func getKubernetesConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to get cluster config: %w", err)
	}
	return config, nil
}

// getCurrentNamespace determines the current namespace from the service account
func getCurrentNamespace() (string, error) {
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", fmt.Errorf("unable to fetch current namespace: %w", err)
	}
	return string(nsBytes), nil
}

// cachedSecretsFetcher caches secrets for improved performance
type cachedSecretsFetcher struct {
	cachedKeyring DockerKeyring
}

// GetKeyring returns the pre-cached keyring
func (c cachedSecretsFetcher) GetKeyring(ctx context.Context) (DockerKeyring, error) {
	return c.cachedKeyring, nil
}

// pluginDockerKeyring is a DockerKeyring implementation that uses credential provider plugins
type pluginDockerKeyring struct{}

// Lookup implements DockerKeyring for credential provider plugins
func (dk *pluginDockerKeyring) Lookup(image string) ([]*cri.AuthConfig, bool) {
	auth, err := GetCredentialFromPlugin(image)
	if err != nil {
		klog.Warningf("Error getting credentials from plugin for image %s: %v", image, err)
		return nil, false
	}

	if auth != nil {
		klog.V(2).Infof("Found credentials for image %s using credential provider plugin", image)
		return []*cri.AuthConfig{auth}, true
	}

	return nil, false
}

// CreateStoreOrDie creates a credential store for container registry authentication
func CreateStoreOrDie(pluginConfigFile, pluginBinDir, nodePluginSA string, enableCache bool) Store {
	// Initialize components
	fetcher := initializeSecretFetcher(nodePluginSA, enableCache)
	pluginsEnabled := initializeCredentialPlugins(pluginConfigFile, pluginBinDir)

	// Create and return the credential store
	return credentialStore{
		secretsFetcher: fetcher,
		pluginsEnabled: pluginsEnabled,
	}
}

// initializeSecretFetcher sets up the Kubernetes secret fetcher with optional caching
func initializeSecretFetcher(nodePluginSA string, enableCache bool) keyringProvider {
	if nodePluginSA == "" {
		return nil
	}

	// Create the basic secret fetcher
	secretFetch, err := createSecretFetcher(nodePluginSA)
	if err != nil {
		klog.Fatalf("Unable to create secret fetcher: %v", err)
		return nil
	}

	// Use caching if enabled
	if enableCache {
		return createCachedFetcher(secretFetch)
	}

	klog.Info("Created dynamic secret store")
	return secretFetch
}

// createCachedFetcher creates a fetcher that caches secrets at startup
func createCachedFetcher(fetcher *secretFetcher) keyringProvider {
	// Pre-fetch secrets once at startup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secrets, err := fetcher.Fetch(ctx)
	if err != nil {
		klog.Warningf("Unable to pre-fetch secrets: %v", err)
		secrets = []corev1.Secret{}
	}

	keyring, err := makeDockerKeyringFromSecrets(secrets)
	if err != nil {
		klog.Warningf("Error creating keyring from pre-fetched secrets: %v", err)
		keyring = NewDockerKeyring()
	}

	klog.Info("Created cached secret store")
	return &cachedSecretsFetcher{cachedKeyring: keyring}
}

// initializeCredentialPlugins sets up credential provider plugins
func initializeCredentialPlugins(configFile, binDir string) bool {
	if len(configFile) == 0 || len(binDir) == 0 {
		return false
	}

	klog.Infof("Registering credential provider plugins using config %s and binary dir %s",
		configFile, binDir)

	if err := RegisterCredentialProviderPlugins(configFile, binDir); err != nil {
		klog.Errorf("Failed to register credential provider plugins: %v", err)
		return false
	}

	return true
}
