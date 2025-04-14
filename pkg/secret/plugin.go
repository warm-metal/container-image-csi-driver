package secret

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// CredentialProviderConfig represents the overall configuration for
// credential provider plugins
type CredentialProviderConfig struct {
	// Kind is the type of credential provider configuration (e.g., CredentialProviderConfig)
	Kind string `json:"kind"`
	// APIVersion is the API version of this configuration (e.g., credentialprovider.kubelet.k8s.io/v1)
	APIVersion string `json:"apiVersion"`
	// Providers is a list of credential provider plugin configurations
	Providers []CredentialProvider `json:"providers"`
}

// CredentialProvider represents the configuration for a single credential provider plugin
type CredentialProvider struct {
	// Name is the required name of the credential provider. It must match the name of the
	// provider executable in the plugin directory.
	Name string `json:"name"`
	// APIVersion is the preferred API version of the credential provider plugin.
	APIVersion string `json:"apiVersion,omitempty"`
	// Args are the optional command-line arguments to pass to the plugin.
	Args []string `json:"args,omitempty"`
	// Env are the optional environment variables to set for the plugin.
	Env []EnvVar `json:"env,omitempty"`
}

// EnvVar represents an environment variable present in a Container.
type EnvVar struct {
	// Name of the environment variable.
	Name string `json:"name"`
	// Value of the environment variable.
	Value string `json:"value,omitempty"`
}

// DockerCredentialHelperOutput represents the output format from docker-credential-helper tools
// See: https://github.com/docker/docker-credential-helpers/blob/master/credentials/credentials.go
type DockerCredentialHelperOutput struct {
	ServerURL string `json:"ServerURL"`
	Username  string `json:"Username"`
	Secret    string `json:"Secret"`
}

var (
	// registeredPlugins contains the list of registered plugins
	registeredPlugins     = make(map[string]PluginConfig)
	registeredPluginsLock sync.RWMutex
)

// PluginConfig contains the information needed to invoke a credential provider plugin
type PluginConfig struct {
	Name       string
	Executable string
	Args       []string
	Env        []EnvVar
	APIVersion string
}

// RegisterCredentialProviderPlugins reads the specified config file and registers
// the external credential provider plugins
func RegisterCredentialProviderPlugins(configFilePath, executableDir string) error {
	// Check if the config file exists
	if _, err := os.Stat(configFilePath); err != nil {
		return fmt.Errorf("failed to stat credential provider config file: %s: %w", configFilePath, err)
	}

	// Read the config file
	configBytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to read credential provider config file %s: %w", configFilePath, err)
	}

	// Parse the config
	config := CredentialProviderConfig{}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return fmt.Errorf("failed to parse credential provider config file %s: %w", configFilePath, err)
	}

	// Register each provider
	for _, provider := range config.Providers {
		executable := filepath.Join(executableDir, provider.Name)

		// Check if the executable exists and is executable
		if info, err := os.Stat(executable); err != nil {
			klog.Warningf("Failed to find credential provider %s at path %s: %v", provider.Name, executable, err)
			continue
		} else if info.IsDir() {
			klog.Warningf("Credential provider %s is a directory, not an executable", executable)
			continue
		}

		// Register the plugin
		registeredPluginsLock.Lock()
		registeredPlugins[provider.Name] = PluginConfig{
			Name:       provider.Name,
			Executable: executable,
			Args:       provider.Args,
			Env:        provider.Env,
			APIVersion: provider.APIVersion,
		}
		registeredPluginsLock.Unlock()

		klog.Infof("Registered credential provider %s at path %s", provider.Name, executable)
	}

	return nil
}

// GetCredentialFromPlugin attempts to get credentials from registered plugins
func GetCredentialFromPlugin(image string) (*cri.AuthConfig, error) {
	registeredPluginsLock.RLock()
	defer registeredPluginsLock.RUnlock()

	klog.V(4).Infof("Looking for credentials from plugins for image: %s", image)

	if len(registeredPlugins) == 0 {
		klog.V(4).Info("No credential provider plugins registered")
		return nil, nil
	}

	// Try each registered plugin
	for name, plugin := range registeredPlugins {
		klog.V(4).Infof("Trying credential plugin %s for image %s", name, image)

		var auth *cri.AuthConfig
		var err error

		// Handle different plugin types
		if isDockerCredentialHelper(plugin.Executable) {
			auth, err = callDockerCredentialHelper(plugin, image)
		} else {
			auth, err = callCustomPlugin(plugin, image)
		}

		if err != nil {
			klog.V(2).Infof("Plugin %s failed: %v", name, err)
			continue
		}

		if auth != nil {
			klog.V(3).Infof("Plugin %s returned valid credentials for image", name)
			return auth, nil
		}
	}

	klog.V(4).Infof("No credentials found from any plugin for image %s", image)
	return nil, nil
}

// isDockerCredentialHelper determines if the executable is a Docker credential helper
func isDockerCredentialHelper(executable string) bool {
	return strings.HasPrefix(filepath.Base(executable), "docker-credential-")
}

// isECRCredentialHelper determines if the executable is the AWS ECR credential helper
func isECRCredentialHelper(executable string) bool {
	base := filepath.Base(executable)
	return strings.HasSuffix(base, "ecr-login") || strings.Contains(base, "ecr-credential-helper")
}

// callDockerCredentialHelper executes a Docker-style credential helper
// See: https://github.com/docker/docker-credential-helpers/blob/master/credentials/credentials.go
func callDockerCredentialHelper(plugin PluginConfig, image string) (*cri.AuthConfig, error) {
	klog.V(4).Infof("Executing Docker credential helper: %s for image %s", plugin.Name, image)

	// Extract server URL from image
	serverURL, err := extractServerURL(image)
	if err != nil {
		return nil, fmt.Errorf("failed to extract server URL from image %s: %w", image, err)
	}

	// Docker credential helpers expect the server URL without https:// prefix
	inputURL := strings.TrimPrefix(serverURL, "https://")
	klog.V(4).Infof("Using server URL for credential lookup: %s", inputURL)

	// Handle ECR-specific environment setup
	isECRHelper := isECRCredentialHelper(plugin.Executable)
	if isECRHelper {
		enrichECREnvironment(plugin.Name, &plugin, serverURL)
	}

	// Execute the credential helper with get command
	output, stdErr, err := executeCredentialHelper(plugin, inputURL)
	if err != nil {
		// Check for common credential helper errors
		if stdErr != "" {
			if strings.Contains(stdErr, "credentials not found") ||
				strings.Contains(stdErr, "not found") {
				klog.V(4).Infof("Plugin %s: no credentials found for %s", plugin.Name, inputURL)
				return nil, nil
			}
			// Only log stderr at higher verbosity level for real errors
			klog.V(2).Infof("Plugin %s stderr output: %s", plugin.Name, stdErr)
		}
		return nil, fmt.Errorf("failed to execute plugin %s: %w", plugin.Name, err)
	}

	if len(output) == 0 {
		klog.V(4).Infof("Plugin %s returned empty output", plugin.Name)
		return nil, nil
	}

	// Parse the credential helper output
	auth, err := parseDockerCredentialHelperOutput(plugin.Name, output, serverURL)
	if err != nil {
		return nil, err
	}

	return auth, nil
}

// enrichECREnvironment adds AWS-specific environment variables for ECR helpers
// Based on: https://github.com/awslabs/amazon-ecr-credential-helper
func enrichECREnvironment(pluginName string, plugin *PluginConfig, serverURL string) {
	// If AWS_REGION is not set, try to extract it from the ECR URL
	if hasEnv(plugin.Env, "AWS_REGION") || os.Getenv("AWS_REGION") != "" {
		return // Region already set
	}

	// Try to extract region from ECR URL
	// Format: account.dkr.ecr.region.amazonaws.com
	if strings.Contains(serverURL, ".dkr.ecr.") && strings.Contains(serverURL, ".amazonaws.com") {
		parts := strings.Split(strings.TrimPrefix(serverURL, "https://"), ".")
		if len(parts) >= 4 && parts[1] == "dkr" && parts[2] == "ecr" {
			region := parts[3]
			klog.V(2).Infof("Extracted AWS region %s from ECR URL for plugin %s", region, pluginName)
			plugin.Env = append(plugin.Env, EnvVar{Name: "AWS_REGION", Value: region})
		}
	}
}

// hasEnv checks if an environment variable exists in the plugin environment
func hasEnv(env []EnvVar, name string) bool {
	for _, e := range env {
		if e.Name == name {
			return true
		}
	}
	return false
}

// executeCredentialHelper runs the credential helper and returns its output
func executeCredentialHelper(plugin PluginConfig, serverURL string) ([]byte, string, error) {
	// Docker credential helpers expect the "get" command
	cmd := exec.Command(plugin.Executable, "get")

	// Set up pipes for stdin/stdout/stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set environment variables
	cmd.Env = os.Environ()
	for _, env := range plugin.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", env.Name, env.Value))
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, stderr.String(), fmt.Errorf("failed to start: %w", err)
	}

	// Write server URL to stdin
	if _, err := stdin.Write([]byte(serverURL + "\n")); err != nil {
		_ = cmd.Process.Kill() // Kill process on error
		return nil, stderr.String(), fmt.Errorf("failed to write to stdin: %w", err)
	}
	stdin.Close()

	// Wait for the command to complete
	if err := cmd.Wait(); err != nil {
		return nil, stderr.String(), err
	}

	return []byte(stdout.String()), stderr.String(), nil
}

// parseDockerCredentialHelperOutput processes the output from a Docker credential helper
func parseDockerCredentialHelperOutput(pluginName string, output []byte, serverURL string) (*cri.AuthConfig, error) {
	var helperOutput DockerCredentialHelperOutput

	if err := json.Unmarshal(output, &helperOutput); err != nil {
		klog.V(2).Infof("Failed to parse plugin %s output: %v", pluginName, err)
		return nil, fmt.Errorf("failed to parse plugin %s output: %w", pluginName, err)
	}

	// Skip if no credentials were returned
	if helperOutput.Username == "" && helperOutput.Secret == "" {
		klog.V(4).Infof("Plugin %s returned no credentials", pluginName)
		return nil, nil
	}

	// Create CRI AuthConfig from helper output
	auth := &cri.AuthConfig{
		Username: helperOutput.Username,
		Password: helperOutput.Secret,
	}

	// Use the server URL from the plugin response if available
	// Otherwise use the server URL we extracted from the image reference
	if helperOutput.ServerURL != "" {
		auth.ServerAddress = helperOutput.ServerURL
	} else {
		auth.ServerAddress = serverURL
	}

	// Set the Auth field (base64 encoded USERNAME:PASSWORD) as required by CRI spec
	// See: https://github.com/containerd/containerd/blob/main/docs/cri/registry.md
	if auth.Username != "" && auth.Password != "" {
		authStr := fmt.Sprintf("%s:%s", auth.Username, auth.Password)
		auth.Auth = base64.StdEncoding.EncodeToString([]byte(authStr))
	}

	klog.V(4).Infof("Plugin %s returned credentials with username: %s", pluginName, auth.Username)
	return auth, nil
}

// callCustomPlugin executes a custom credential plugin that uses the --image parameter
func callCustomPlugin(plugin PluginConfig, image string) (*cri.AuthConfig, error) {
	klog.V(4).Infof("Executing custom credential plugin: %s for image %s", plugin.Name, image)

	// Set up the command with args
	cmd := exec.Command(plugin.Executable)
	cmd.Args = append(cmd.Args, plugin.Args...)
	cmd.Args = append(cmd.Args, "--image="+image)

	// Set environment variables
	cmd.Env = os.Environ()
	for _, env := range plugin.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", env.Name, env.Value))
	}

	// Set up stderr capture
	var stderr strings.Builder
	cmd.Stderr = &stderr

	// Execute the command
	output, err := cmd.Output()
	if err != nil {
		stderrOutput := stderr.String()
		if stderrOutput != "" {
			klog.V(2).Infof("Plugin %s stderr output: %s", plugin.Name, stderrOutput)
		}
		return nil, fmt.Errorf("failed to execute plugin %s: %w", plugin.Name, err)
	}

	return parseCustomPluginOutput(plugin.Name, output)
}

// parseCustomPluginOutput processes the output from a custom credential plugin
func parseCustomPluginOutput(pluginName string, output []byte) (*cri.AuthConfig, error) {
	// Don't log output details as they may contain credentials
	klog.V(4).Infof("Plugin %s returned output", pluginName)

	// Parse the JSON output
	var response struct {
		Auth *cri.AuthConfig `json:"auth"`
	}

	// Trim any leading/trailing whitespace
	outputStr := strings.TrimSpace(string(output))
	if err := json.Unmarshal([]byte(outputStr), &response); err != nil {
		return nil, fmt.Errorf("failed to parse plugin %s output: %w", pluginName, err)
	}

	// If no auth was returned, or it's empty
	if response.Auth == nil {
		klog.V(4).Infof("Plugin %s returned no credentials", pluginName)
		return nil, nil
	}

	// Add Auth field if not already set (base64 encoded USERNAME:PASSWORD)
	if response.Auth.Auth == "" && response.Auth.Username != "" && response.Auth.Password != "" {
		authStr := fmt.Sprintf("%s:%s", response.Auth.Username, response.Auth.Password)
		response.Auth.Auth = base64.StdEncoding.EncodeToString([]byte(authStr))
	}

	klog.V(4).Infof("Plugin %s returned credentials with username: %s",
		pluginName, response.Auth.Username)

	return response.Auth, nil
}

// extractServerURL extracts the server/registry URL from an image reference
// For example, "672327909798.dkr.ecr.us-east-1.amazonaws.com/warm-metal/ecr-test-image"
// would return "https://672327909798.dkr.ecr.us-east-1.amazonaws.com"
func extractServerURL(image string) (string, error) {
	// Handle image references with and without tags/digests
	// First handle ":" for tags and "@" for digests
	imagePart := strings.Split(strings.Split(image, "@")[0], ":")[0]

	// Format: [registry/]repository
	parts := strings.Split(imagePart, "/")
	if len(parts) == 1 {
		// No registry specified, assume Docker Hub
		return "https://index.docker.io", nil
	}

	// Check if the first part looks like a registry (contains "." or ":")
	if strings.ContainsAny(parts[0], ".:") {
		// It's a registry
		return "https://" + parts[0], nil
	}

	// Check if this is a Docker Hub namespaced repository
	if len(parts) >= 2 && !strings.ContainsAny(parts[0], ".:") {
		return "https://index.docker.io", nil
	}

	return "", fmt.Errorf("could not extract server URL from image: %s", image)
}
