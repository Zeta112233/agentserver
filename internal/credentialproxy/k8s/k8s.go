package k8s

import (
	"net/http"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
)

// AllowPrivateUpstreams disables the SSRF dial-time guard when true.
// Set by cmd/credentialproxy/main.go from config.
var AllowPrivateUpstreams bool

// K8sProvider implements provider.Provider for Kubernetes API servers.
type K8sProvider struct{}

func init() {
	provider.Register("k8s", &K8sProvider{})
}

func (p *K8sProvider) Kind() string { return "k8s" }

func (p *K8sProvider) ParseUpload(contentType string, raw []byte) (*provider.UploadResult, error) {
	return ParseKubeconfig(contentType, raw)
}

func (p *K8sProvider) BuildSandboxConfig(
	bindings []*provider.BindingMeta,
	proxyToken string,
	credproxyPublicURL string,
) ([]*provider.SandboxConfigFile, error) {
	file, err := BuildMergedKubeconfig(bindings, proxyToken, credproxyPublicURL)
	if err != nil {
		return nil, err
	}
	return []*provider.SandboxConfigFile{file}, nil
}

func (p *K8sProvider) ServeHTTP(w http.ResponseWriter, r *http.Request, b *provider.DecryptedBinding) {
	serveK8sProxy(w, r, b, AllowPrivateUpstreams)
}
