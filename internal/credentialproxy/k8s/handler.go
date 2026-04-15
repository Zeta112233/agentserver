package k8s

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
	utilproxy "k8s.io/apimachinery/pkg/util/proxy"
)

// serveK8sProxy handles a single proxied request to an upstream K8s apiserver.
func serveK8sProxy(w http.ResponseWriter, r *http.Request, binding *provider.DecryptedBinding, allowPrivateUpstreams bool) {
	transport, err := buildUpstreamTransport(binding, allowPrivateUpstreams)
	if err != nil {
		http.Error(w, "upstream transport error", http.StatusBadGateway)
		return
	}

	upstream, err := url.Parse(binding.ServerURL)
	if err != nil {
		http.Error(w, "invalid upstream URL", http.StatusInternalServerError)
		return
	}

	// Strip the /k8s/{binding_id} prefix from the request path.
	prefix := fmt.Sprintf("/k8s/%s", binding.ID)
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "internal routing error: unexpected path prefix", http.StatusInternalServerError)
		return
	}
	r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	// Also strip from RawPath if present.
	if r.URL.RawPath != "" {
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, prefix)
		if r.URL.RawPath == "" {
			r.URL.RawPath = "/"
		}
	}

	// Remove the sandbox's proxy_token from the Authorization header.
	r.Header.Del("Authorization")

	// Inject real credentials based on auth type.
	switch binding.AuthType {
	case "bearer":
		if err := injectBearerAuth(r, binding); err != nil {
			http.Error(w, "credential injection error", http.StatusInternalServerError)
			return
		}
	case "oidc":
		token, err := getOIDCBearerToken(r.Context(), binding.ID, binding.AuthSecret)
		if err != nil {
			http.Error(w, "oidc token refresh failed", http.StatusBadGateway)
			return
		}
		r.Header.Set("Authorization", "Bearer "+token)
	case "client_cert":
		// Credentials are in the TLS transport -- no header needed.
	default:
		http.Error(w, "unsupported auth type", http.StatusInternalServerError)
		return
	}

	handler := utilproxy.NewUpgradeAwareHandler(upstream, transport, false, false, &errorResponder{})
	handler.ServeHTTP(w, r)
}

// errorResponder implements utilproxy.ErrorResponder for upgrade-aware proxy errors.
type errorResponder struct{}

func (e *errorResponder) Error(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, "upstream unreachable", http.StatusBadGateway)
}
