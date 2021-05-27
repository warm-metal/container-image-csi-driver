package secret

import (
	"context"
	"encoding/json"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/credentialprovider"
	execplugin "k8s.io/kubernetes/pkg/credentialprovider/plugin"
	"time"

	// register credential providers
	_ "k8s.io/kubernetes/pkg/credentialprovider/aws"
	_ "k8s.io/kubernetes/pkg/credentialprovider/azure"
	_ "k8s.io/kubernetes/pkg/credentialprovider/gcp"
)

type Cache interface {
	GetDockerKeyring(ctx context.Context, secrets map[string]string) (credentialprovider.DockerKeyring, error)
}

const daemonSA = "csi-image-warm-metal"

func CreateCacheOrDie(pluginConfigFile, pluginBinDir string) Cache {
	if len(pluginConfigFile) > 0 && len(pluginBinDir) > 0 {
		if err := execplugin.RegisterCredentialProviderPlugins(pluginConfigFile, pluginBinDir); err != nil {
			klog.Fatalf("unable to register the credential plugin through %q and %q: %s", pluginConfigFile,
				pluginBinDir, err)
		}
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("unable to get cluster config: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("unable to get cluster client: %s", err)
	}

	c := secretWOCache{
		k8sCliSet: clientset,
		keyring:   credentialprovider.NewDockerKeyring(),
	}

	curNamespace, err := ioutil.ReadFile("/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		klog.Warningf("unable to fetch the current namespace from the sa volume: %q", err.Error())
		return c
	}

	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()
	namespace := string(curNamespace)
	sa, err := clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, daemonSA, metav1.GetOptions{})
	if err != nil {
		klog.Errorf(`unable to fetch service account of the daemon pod "%s/%s": %s`, namespace, daemonSA, err)
		return c
	}

	c.daemonSecrets = make([]corev1.Secret, len(sa.ImagePullSecrets))
	klog.Infof(
		`got %d imagePullSecrets from the service account %s/%s`, len(sa.ImagePullSecrets), namespace, daemonSA,
	)

	for i := range sa.ImagePullSecrets {
		s := &sa.ImagePullSecrets[i]
		secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, s.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf(`unable to fetch secret "%s/%s": %s`, namespace, s.Name, err)
			continue
		}

		c.daemonSecrets[i] = *secret
	}

	return c
}

type secretWOCache struct {
	k8sCliSet     *kubernetes.Clientset
	daemonSecrets []corev1.Secret
	keyring       credentialprovider.DockerKeyring
}

func (s secretWOCache) GetDockerKeyring(ctx context.Context, secretData map[string]string) (keyring credentialprovider.DockerKeyring, err error) {
	keyRing, err := s.makeDockerKeyring(s.daemonSecrets, secretData)
	if err != nil {
		klog.Errorf("unable to create keyring: %s", err)
	}

	return keyRing, err
}

func (s secretWOCache) makeDockerKeyring(passedSecrets []corev1.Secret, secretData map[string]string) (credentialprovider.DockerKeyring, error) {
	passedCredentials := make([]credentialprovider.DockerConfig, 0, len(passedSecrets)+len(secretData))
	if len(secretData) > 0 {
		cred, err := parseDockerConfigFromSecretData(stringSecretData(secretData))
		if err != nil {
			return nil, err
		}

		passedCredentials = append(passedCredentials, cred)
	}

	for _, passedSecret := range passedSecrets {
		if len(passedSecret.Data) == 0 {
			continue
		}

		cred, err := parseDockerConfigFromSecretData(byteSecretData(passedSecret.Data))
		if err != nil {
			return nil, err
		}

		passedCredentials = append(passedCredentials, cred)
	}

	if len(passedCredentials) > 0 {
		basicKeyring := &credentialprovider.BasicDockerKeyring{}
		for _, currCredentials := range passedCredentials {
			basicKeyring.Add(currCredentials)
		}
		return credentialprovider.UnionDockerKeyring{basicKeyring, s.keyring}, nil
	}

	return s.keyring, nil
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
