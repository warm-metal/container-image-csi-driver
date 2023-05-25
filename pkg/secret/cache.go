package secret

import (
	"context"
	"encoding/json"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
	execplugin "k8s.io/kubernetes/pkg/credentialprovider/plugin"

	// register credential providers
	_ "k8s.io/kubernetes/pkg/credentialprovider/aws"
	_ "k8s.io/kubernetes/pkg/credentialprovider/azure"
	_ "k8s.io/kubernetes/pkg/credentialprovider/gcp"
)

type Store interface {
	GetDockerKeyring(ctx context.Context, secrets map[string]string) (credentialprovider.DockerKeyring, error)
}

func makeDockerKeyringFromSecrets(secrets []corev1.Secret) (credentialprovider.DockerKeyring, error) {
	keyring := &credentialprovider.BasicDockerKeyring{}
	for _, secret := range secrets {
		if len(secret.Data) == 0 {
			continue
		}

		cred, err := parseDockerConfigFromSecretData(byteSecretData(secret.Data))
		if err != nil {
			klog.Errorf(`unable to parse secret %s, %#v`, err, secret)
			return nil, err
		}

		keyring.Add(cred)
	}

	return keyring, nil
}

func makeDockerKeyringFromMap(secretData map[string]string) (credentialprovider.DockerKeyring, error) {
	keyring := &credentialprovider.BasicDockerKeyring{}
	if len(secretData) > 0 {
		cred, err := parseDockerConfigFromSecretData(stringSecretData(secretData))
		if err != nil {
			klog.Errorf(`unable to parse secret data %s, %#v`, err, secretData)
			return nil, err
		}

		keyring.Add(cred)
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
	strings, existed := s[key]
	if existed {
		data = []byte(strings)
	}

	return
}

func parseDockerConfigFromSecretData(data secretDataWrapper) (credentialprovider.DockerConfig, error) {
	if dockerConfigJSONBytes, existed := data.Get(corev1.DockerConfigJsonKey); existed {
		if len(dockerConfigJSONBytes) > 0 {
			dockerConfigJSON := credentialprovider.DockerConfigJSON{}
			if err := json.Unmarshal(dockerConfigJSONBytes, &dockerConfigJSON); err != nil {
				return nil, err
			}

			return dockerConfigJSON.Auths, nil
		}
	}

	if dockercfgBytes, existed := data.Get(corev1.DockerConfigKey); existed {
		if len(dockercfgBytes) > 0 {
			dockercfg := credentialprovider.DockerConfig{}
			if err := json.Unmarshal(dockercfgBytes, &dockercfg); err != nil {
				return nil, err
			}
			return dockercfg, nil
		}
	}

	return nil, nil
}

type persistentKeyringGetter interface {
	Get(context.Context) credentialprovider.DockerKeyring
}

type keyringStore struct {
	persistentKeyringGetter
}

func (s keyringStore) GetDockerKeyring(ctx context.Context, secretData map[string]string) (keyring credentialprovider.DockerKeyring, err error) {
	var preferredKeyring credentialprovider.DockerKeyring
	if len(secretData) > 0 {
		preferredKeyring, err = makeDockerKeyringFromMap(secretData)
		if err != nil {
			return nil, err
		}
	}

	daemonKeyring := s.Get(ctx)
	if preferredKeyring != nil {
		return credentialprovider.UnionDockerKeyring{preferredKeyring, daemonKeyring}, nil
	}

	return daemonKeyring, err
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

	secrets := make([]corev1.Secret, len(sa.ImagePullSecrets))
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

		secrets[i] = *secret
	}

	return secrets, nil
}

func (s secretFetcher) Get(ctx context.Context) credentialprovider.DockerKeyring {
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

	curNamespace, err := os.ReadFile("/run/secrets/kubernetes.io/serviceaccount/namespace")
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
	daemonKeyring credentialprovider.DockerKeyring
}

func (s secretWOCache) Get(_ context.Context) credentialprovider.DockerKeyring {
	return s.daemonKeyring
}

func createCacheOrDie(nodePluginSA string) Store {
	fetcher := createSecretFetcher(nodePluginSA)
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	var keyring credentialprovider.DockerKeyring
	secrets, _ := fetcher.Fetch(ctx)
	keyring, _ = makeDockerKeyringFromSecrets(secrets)
	return keyringStore{
		persistentKeyringGetter: secretWOCache{
			daemonKeyring: keyring,
		},
	}
}

func CreateStoreOrDie(pluginConfigFile, pluginBinDir, nodePluginSA string, enableCache bool) Store {
	if len(pluginConfigFile) > 0 && len(pluginBinDir) > 0 {
		if err := execplugin.RegisterCredentialProviderPlugins(pluginConfigFile, pluginBinDir); err != nil {
			klog.Fatalf("unable to register the credential plugin through %q and %q: %s", pluginConfigFile,
				pluginBinDir, err)
		}
	}

	if enableCache {
		return createCacheOrDie(nodePluginSA)
	} else {
		return createFetcherOrDie(nodePluginSA)
	}
}
