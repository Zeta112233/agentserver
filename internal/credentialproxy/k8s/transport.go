package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
)

// buildUpstreamTransport creates an *http.Transport configured for the upstream
// K8s apiserver, with TLS settings from the binding and SSRF dial-time guard.
func buildUpstreamTransport(binding *provider.DecryptedBinding, allowPrivateUpstreams bool) (*http.Transport, error) {
	tlsCfg := &tls.Config{}

	// Configure CA pool from public_meta.
	if caB64, ok := binding.PublicMeta["ca_data_b64"].(string); ok && caB64 != "" {
		caData, err := base64.StdEncoding.DecodeString(caB64)
		if err != nil {
			return nil, fmt.Errorf("decode CA data: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.RootCAs = pool
	}

	// Configure client certificate if auth_type is client_cert.
	if binding.AuthType == "client_cert" {
		var certData struct {
			CertPEM string `json:"cert_pem"`
			KeyPEM  string `json:"key_pem"`
		}
		if err := json.Unmarshal(binding.AuthSecret, &certData); err != nil {
			return nil, fmt.Errorf("unmarshal client cert: %w", err)
		}
		cert, err := tls.X509KeyPair([]byte(certData.CertPEM), []byte(certData.KeyPEM))
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	transport := &http.Transport{
		TLSClientConfig:       tlsCfg,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}

	// SSRF layer 2: dial-time DNS resolution guard.
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split host port: %w", err)
		}

		if !allowPrivateUpstreams {
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}
			for _, ip := range ips {
				if IsPrivateIP(ip.IP) {
					return nil, fmt.Errorf("dial refused: upstream address %s resolved to private IP %s", host, ip.IP)
				}
			}
		}

		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}

	return transport, nil
}

// bearerToken extracts the bearer token from a DecryptedBinding's AuthSecret.
func bearerToken(binding *provider.DecryptedBinding) (string, error) {
	var tokenData struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(binding.AuthSecret, &tokenData); err != nil {
		return "", fmt.Errorf("unmarshal bearer token: %w", err)
	}
	return tokenData.Token, nil
}

// injectBearerAuth adds the real bearer token to the request, replacing the proxy token.
func injectBearerAuth(r *http.Request, binding *provider.DecryptedBinding) error {
	token, err := bearerToken(binding)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return nil
}
