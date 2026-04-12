package k8s

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ParseKubeconfig validates an uploaded kubeconfig yaml and extracts the fields to persist.
func ParseKubeconfig(_ string, raw []byte) (*provider.UploadResult, error) {
	cfg, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid kubeconfig: %w", err)
	}

	clusterName, cluster, err := resolveCluster(cfg)
	if err != nil {
		return nil, err
	}

	userName, err := resolveUser(cfg, clusterName)
	if err != nil {
		return nil, err
	}
	userInfo := cfg.AuthInfos[userName]
	if userInfo == nil {
		return nil, fmt.Errorf("user %q not found in kubeconfig", userName)
	}

	// Validate server URL.
	serverURL, err := validateServerURL(cluster.Server)
	if err != nil {
		return nil, err
	}

	// Extract and validate CA.
	caDataB64, err := extractCA(cluster)
	if err != nil {
		return nil, err
	}

	// Extract auth credentials.
	authType, authSecret, err := extractAuth(userInfo)
	if err != nil {
		return nil, err
	}

	displayName := clusterName

	publicMeta := map[string]any{}
	if caDataB64 != "" {
		publicMeta["ca_data_b64"] = caDataB64
	}

	return &provider.UploadResult{
		DisplayName: displayName,
		ServerURL:   serverURL,
		PublicMeta:  publicMeta,
		AuthType:    authType,
		AuthSecret:  authSecret,
	}, nil
}

// resolveCluster picks the single cluster to use from the kubeconfig.
func resolveCluster(cfg *clientcmdapi.Config) (string, *clientcmdapi.Cluster, error) {
	// If current-context is set, use it.
	if cfg.CurrentContext != "" {
		ctx, ok := cfg.Contexts[cfg.CurrentContext]
		if !ok {
			return "", nil, fmt.Errorf("current-context %q not found", cfg.CurrentContext)
		}
		cluster, ok := cfg.Clusters[ctx.Cluster]
		if !ok {
			return "", nil, fmt.Errorf("cluster %q (from context %q) not found", ctx.Cluster, cfg.CurrentContext)
		}
		return ctx.Cluster, cluster, nil
	}

	// If exactly one cluster, use it.
	if len(cfg.Clusters) == 1 {
		for name, cluster := range cfg.Clusters {
			return name, cluster, nil
		}
	}

	return "", nil, fmt.Errorf("kubeconfig has %d clusters and no current-context; set current-context or provide a single-cluster kubeconfig", len(cfg.Clusters))
}

// resolveUser finds the user associated with the resolved cluster's context.
func resolveUser(cfg *clientcmdapi.Config, clusterName string) (string, error) {
	// Find a context referencing this cluster.
	if cfg.CurrentContext != "" {
		ctx := cfg.Contexts[cfg.CurrentContext]
		if ctx != nil {
			return ctx.AuthInfo, nil
		}
	}
	for _, ctx := range cfg.Contexts {
		if ctx.Cluster == clusterName {
			return ctx.AuthInfo, nil
		}
	}
	// If exactly one user, use it.
	if len(cfg.AuthInfos) == 1 {
		for name := range cfg.AuthInfos {
			return name, nil
		}
	}
	return "", fmt.Errorf("cannot resolve user for cluster %q", clusterName)
}

func validateServerURL(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid server URL %q: %w", server, err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("server URL must use https, got %q", u.Scheme)
	}

	// Check for literal private IPs.
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return "", fmt.Errorf("server URL %q points to a private IP address", server)
		}
	}

	return server, nil
}

func extractCA(cluster *clientcmdapi.Cluster) (string, error) {
	if cluster.CertificateAuthority != "" {
		return "", fmt.Errorf("certificate-authority (file path) is not supported; use certificate-authority-data (inline base64) instead")
	}
	if len(cluster.CertificateAuthorityData) == 0 {
		return "", nil // no CA specified, will use system roots
	}
	return base64.StdEncoding.EncodeToString(cluster.CertificateAuthorityData), nil
}

func extractAuth(user *clientcmdapi.AuthInfo) (string, []byte, error) {
	if user.Exec != nil {
		return "", nil, fmt.Errorf("exec plugin is not supported; create a dedicated ServiceAccount and upload its static-token kubeconfig")
	}
	if user.AuthProvider != nil {
		return "", nil, fmt.Errorf("auth-provider is not supported; create a dedicated ServiceAccount and upload its static-token kubeconfig")
	}
	if user.Username != "" || user.Password != "" {
		return "", nil, fmt.Errorf("basic auth (username/password) is not supported; use a bearer token or client certificate")
	}

	// Bearer token.
	if user.Token != "" {
		secret, err := json.Marshal(map[string]string{"token": user.Token})
		if err != nil {
			return "", nil, fmt.Errorf("marshal token: %w", err)
		}
		return "bearer", secret, nil
	}

	// Client certificate.
	if len(user.ClientCertificateData) > 0 && len(user.ClientKeyData) > 0 {
		secret, err := json.Marshal(map[string]string{
			"cert_pem": string(user.ClientCertificateData),
			"key_pem":  string(user.ClientKeyData),
		})
		if err != nil {
			return "", nil, fmt.Errorf("marshal client cert: %w", err)
		}
		return "client_cert", secret, nil
	}

	if user.ClientCertificate != "" || user.ClientKey != "" {
		return "", nil, fmt.Errorf("client-certificate/client-key (file paths) are not supported; use client-certificate-data/client-key-data (inline)")
	}

	return "", nil, fmt.Errorf("no supported auth method found; provide a token or client-certificate-data + client-key-data")
}
