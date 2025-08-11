// Package secret provides functionality for handling container registry authentication.
package secret

import (
	"strings"

	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// DockerConfig represents the config file used by the docker CLI.
// This allows users to authenticate with multiple registries.
type DockerConfig map[string]cri.AuthConfig

// DockerConfigJSON represents the new docker config format that includes
// credential helper configs.
type DockerConfigJSON struct {
	Auths DockerConfig `json:"auths"`
}

// DockerKeyring tracks a set of docker registry credentials.
type DockerKeyring interface {
	// Lookup returns the registry credentials for the specified image.
	Lookup(image string) ([]*cri.AuthConfig, bool)
}

// BasicDockerKeyring is a trivial implementation of DockerKeyring that simply
// wraps a map of registry credentials.
type BasicDockerKeyring struct {
	Configs []DockerConfig
}

// UnionDockerKeyring is a keyring that consists of multiple keyrings.
type UnionDockerKeyring []DockerKeyring

// Add adds a new entry to the keyring.
func (dk *BasicDockerKeyring) Add(cfg DockerConfig) {
	dk.Configs = append(dk.Configs, cfg)
}

// Lookup implements DockerKeyring.
func (dk *BasicDockerKeyring) Lookup(image string) ([]*cri.AuthConfig, bool) {
	// Strip any tag/digest from the image name - we don't include this
	// when matching against the credentials.
	var registryURL string
	parts := splitImageName(image)
	if len(parts) > 0 {
		registryURL = parts[0]
	}

	if registryURL == "" {
		klog.V(4).Infof("No registry URL found for image: %s", image)
		return nil, false
	}

	klog.V(4).Infof("Looking up credentials for registry: %s", registryURL)
	klog.V(4).Infof("Number of credential configs available: %d", len(dk.Configs))

	var matches []*cri.AuthConfig
	for i, cfg := range dk.Configs {
		klog.V(4).Infof("Checking config %d", i)
		if auth, found := matchRegistry(cfg, registryURL); found {
			// Don't log auth details, only the fact that we found a match
			klog.V(3).Infof("Found matching credentials for %s", registryURL)
			matches = append(matches, auth)
		}
	}

	klog.V(4).Infof("Found %d matching credential(s) for %s", len(matches), registryURL)
	return matches, len(matches) > 0
}

// Lookup implements DockerKeyring.
func (dk UnionDockerKeyring) Lookup(image string) ([]*cri.AuthConfig, bool) {
	var authConfigs []*cri.AuthConfig
	found := false

	// Lookup in all keyrings
	for _, subKeyring := range dk {
		if subKeyring == nil {
			continue
		}

		if configs, ok := subKeyring.Lookup(image); ok {
			authConfigs = append(authConfigs, configs...)
			found = true
		}
	}

	return authConfigs, found
}

// Helper function to split the image name into registry and repository parts
func splitImageName(imageName string) []string {
	// Parse the image name to extract the registry
	parts := strings.Split(imageName, "/")
	if len(parts) == 1 {
		return []string{"docker.io"} // Default to docker hub
	}

	// Check if this is a hostname (contains dots or port)
	if strings.ContainsAny(parts[0], ".:") {
		return []string{parts[0]}
	}

	// Docker Hub with implicit registry
	return []string{"docker.io"}
}

// Helper function to match a registry URL against the Docker config
func matchRegistry(cfg DockerConfig, registryURL string) (*cri.AuthConfig, bool) {
	klog.V(5).Infof("Matching registry URL: %s", registryURL)

	// Direct match first
	if entry, ok := cfg[registryURL]; ok {
		klog.V(5).Infof("Found direct match for %s", registryURL)
		// Create copy to avoid modifying the original
		auth := entry
		auth.ServerAddress = registryURL
		return &auth, true
	}

	// Try with https:// prefix
	httpsRegistry := "https://" + registryURL
	if entry, ok := cfg[httpsRegistry]; ok {
		klog.V(5).Infof("Found match with https:// prefix for %s", registryURL)
		// Create copy to avoid modifying the original
		auth := entry
		auth.ServerAddress = registryURL
		return &auth, true
	}

	// Try with http:// prefix
	httpRegistry := "http://" + registryURL
	if entry, ok := cfg[httpRegistry]; ok {
		klog.V(5).Infof("Found match with http:// prefix for %s", registryURL)
		// Create copy to avoid modifying the original
		auth := entry
		auth.ServerAddress = registryURL
		return &auth, true
	}

	// Try to find a partial match
	for registry, entry := range cfg {
		if strings.Contains(registryURL, registry) || strings.Contains(registry, registryURL) {
			klog.V(5).Infof("Found partial match: config has %s, image uses %s", registry, registryURL)
			// Create copy to avoid modifying the original
			auth := entry
			auth.ServerAddress = registryURL
			return &auth, true
		}
	}

	klog.V(5).Infof("No credential match found for %s", registryURL)
	return nil, false
}

// Helper function to get registry keys for logging
func getRegistryKeys(cfg DockerConfig) []string {
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	return keys
}

// NewDockerKeyring creates a new empty keyring.
func NewDockerKeyring() DockerKeyring {
	return &BasicDockerKeyring{}
}
