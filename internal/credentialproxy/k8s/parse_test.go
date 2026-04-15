package k8s

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return data
}

func TestParseKubeconfig_ValidBearer(t *testing.T) {
	data := readTestdata(t, "valid-bearer.yaml")
	result, err := ParseKubeconfig("", data)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}
	if result.AuthType != "bearer" {
		t.Errorf("auth_type = %q, want %q", result.AuthType, "bearer")
	}
	if result.ServerURL != "https://api.example.com:6443" {
		t.Errorf("server_url = %q", result.ServerURL)
	}
	if result.DisplayName != "my-cluster" {
		t.Errorf("display_name = %q", result.DisplayName)
	}
	if _, ok := result.PublicMeta["ca_data_b64"]; !ok {
		t.Error("expected ca_data_b64 in public_meta")
	}
	if len(result.AuthSecret) == 0 {
		t.Error("expected non-empty auth_secret")
	}
}

func TestParseKubeconfig_ValidClientCert(t *testing.T) {
	data := readTestdata(t, "valid-clientcert.yaml")
	result, err := ParseKubeconfig("", data)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}
	if result.AuthType != "client_cert" {
		t.Errorf("auth_type = %q, want %q", result.AuthType, "client_cert")
	}
	if result.DisplayName != "cert-cluster" {
		t.Errorf("display_name = %q", result.DisplayName)
	}
}

func TestParseKubeconfig_RejectNonOIDCExecPlugin(t *testing.T) {
	data := readTestdata(t, "exec-plugin.yaml")
	_, err := ParseKubeconfig("", data)
	if err == nil {
		t.Fatal("expected error for non-OIDC exec plugin (AWS EKS)")
	}
	if !strings.Contains(err.Error(), "exec plugin") {
		t.Errorf("error should mention exec plugin, got: %v", err)
	}
}

func TestParseKubeconfig_OIDCExecPlugin(t *testing.T) {
	data := readTestdata(t, "oidc-exec.yaml")
	result, err := ParseKubeconfig("", data)
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}
	if result.AuthType != "oidc" {
		t.Errorf("auth_type = %q, want %q", result.AuthType, "oidc")
	}
	if !result.PendingDeviceAuth {
		t.Error("expected PendingDeviceAuth=true for OIDC exec plugin")
	}
	if result.ServerURL != "https://k8s-api.example.com:6443" {
		t.Errorf("server_url = %q", result.ServerURL)
	}
	if result.DisplayName != "k8s-nj-prod" {
		t.Errorf("display_name = %q", result.DisplayName)
	}
	if len(result.AuthSecret) == 0 {
		t.Fatal("expected non-empty auth_secret")
	}

	// Verify the parsed OIDC config.
	var cfg OIDCAuthConfig
	if err := json.Unmarshal(result.AuthSecret, &cfg); err != nil {
		t.Fatalf("unmarshal auth_secret: %v", err)
	}
	if cfg.IssuerURL != "https://connect.cs.ac.cn" {
		t.Errorf("issuer_url = %q", cfg.IssuerURL)
	}
	if cfg.ClientID != "5db2449e-3671-413a-b34f-33226e9187ad" {
		t.Errorf("client_id = %q", cfg.ClientID)
	}
	// Should have openid + offline_access.
	wantScopes := map[string]bool{"openid": true, "offline_access": true}
	for _, s := range cfg.Scopes {
		delete(wantScopes, s)
	}
	if len(wantScopes) > 0 {
		t.Errorf("missing scopes: %v, got: %v", wantScopes, cfg.Scopes)
	}
}

func TestParseKubeconfig_RejectHTTP(t *testing.T) {
	data := readTestdata(t, "http-server.yaml")
	_, err := ParseKubeconfig("", data)
	if err == nil {
		t.Fatal("expected error for http server")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error should mention https, got: %v", err)
	}
}

func TestParseKubeconfig_RejectPrivateIP(t *testing.T) {
	data := readTestdata(t, "private-ip.yaml")
	_, err := ParseKubeconfig("", data)
	if err == nil {
		t.Fatal("expected error for private IP")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Errorf("error should mention private, got: %v", err)
	}
}

func TestParseKubeconfig_RejectBasicAuth(t *testing.T) {
	yaml := `apiVersion: v1
kind: Config
current-context: ctx
clusters:
  - name: c
    cluster:
      server: https://api.example.com:6443
users:
  - name: u
    user:
      username: admin
      password: secret
contexts:
  - name: ctx
    context:
      cluster: c
      user: u`
	_, err := ParseKubeconfig("", []byte(yaml))
	if err == nil {
		t.Fatal("expected error for basic auth")
	}
	if !strings.Contains(err.Error(), "basic auth") {
		t.Errorf("error should mention basic auth, got: %v", err)
	}
}

func TestParseKubeconfig_RejectFilePath(t *testing.T) {
	yaml := `apiVersion: v1
kind: Config
current-context: ctx
clusters:
  - name: c
    cluster:
      server: https://api.example.com:6443
      certificate-authority: /path/to/ca.crt
users:
  - name: u
    user:
      token: my-token
contexts:
  - name: ctx
    context:
      cluster: c
      user: u`
	_, err := ParseKubeconfig("", []byte(yaml))
	if err == nil {
		t.Fatal("expected error for file path CA")
	}
	if !strings.Contains(err.Error(), "certificate-authority-data") {
		t.Errorf("error should mention inline data, got: %v", err)
	}
}

func TestParseKubeconfig_NoAuth(t *testing.T) {
	yaml := `apiVersion: v1
kind: Config
current-context: ctx
clusters:
  - name: c
    cluster:
      server: https://api.example.com:6443
users:
  - name: u
    user: {}
contexts:
  - name: ctx
    context:
      cluster: c
      user: u`
	_, err := ParseKubeconfig("", []byte(yaml))
	if err == nil {
		t.Fatal("expected error for no auth")
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"8.8.8.8", false},
		{"1.2.3.4", false},
		{"::1", true},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid IP: %s", tt.ip)
			}
			got := IsPrivateIP(ip)
			if got != tt.private {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}
