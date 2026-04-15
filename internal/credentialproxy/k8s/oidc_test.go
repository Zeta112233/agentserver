package k8s

import (
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestParseOIDCExecPlugin_ValidEqualsForm(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "kubectl",
		Args: []string{
			"oidc-login", "get-token",
			"--oidc-issuer-url=https://connect.example.com",
			"--oidc-client-id=my-client-id",
			"--grant-type=device-code",
			"--oidc-extra-scope=offline_access",
		},
	}
	cfg, err := parseOIDCExecPlugin(exec)
	if err != nil {
		t.Fatalf("parseOIDCExecPlugin: %v", err)
	}
	if cfg.IssuerURL != "https://connect.example.com" {
		t.Errorf("IssuerURL = %q", cfg.IssuerURL)
	}
	if cfg.ClientID != "my-client-id" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
	if len(cfg.Scopes) < 2 {
		t.Fatalf("expected at least 2 scopes, got %v", cfg.Scopes)
	}
	hasOpenID := false
	hasOffline := false
	for _, s := range cfg.Scopes {
		if s == "openid" {
			hasOpenID = true
		}
		if s == "offline_access" {
			hasOffline = true
		}
	}
	if !hasOpenID {
		t.Error("missing openid scope")
	}
	if !hasOffline {
		t.Error("missing offline_access scope")
	}
}

func TestParseOIDCExecPlugin_ValidSpaceForm(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "kubectl",
		Args: []string{
			"oidc-login", "get-token",
			"--oidc-issuer-url", "https://connect.example.com",
			"--oidc-client-id", "another-client",
		},
	}
	cfg, err := parseOIDCExecPlugin(exec)
	if err != nil {
		t.Fatalf("parseOIDCExecPlugin: %v", err)
	}
	if cfg.IssuerURL != "https://connect.example.com" {
		t.Errorf("IssuerURL = %q", cfg.IssuerURL)
	}
	if cfg.ClientID != "another-client" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
}

func TestParseOIDCExecPlugin_MissingIssuer(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "kubectl",
		Args: []string{
			"oidc-login", "get-token",
			"--oidc-client-id=my-client",
		},
	}
	_, err := parseOIDCExecPlugin(exec)
	if err == nil {
		t.Fatal("expected error for missing issuer URL")
	}
	if !contains(err.Error(), "issuer") {
		t.Errorf("error should mention issuer, got: %v", err)
	}
}

func TestParseOIDCExecPlugin_MissingClientID(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "kubectl",
		Args: []string{
			"oidc-login", "get-token",
			"--oidc-issuer-url=https://example.com",
		},
	}
	_, err := parseOIDCExecPlugin(exec)
	if err == nil {
		t.Fatal("expected error for missing client ID")
	}
	if !contains(err.Error(), "client-id") {
		t.Errorf("error should mention client-id, got: %v", err)
	}
}

func TestParseOIDCExecPlugin_NotOIDCPlugin(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "aws",
		Args:    []string{"eks", "get-token", "--cluster-name", "my-cluster"},
	}
	_, err := parseOIDCExecPlugin(exec)
	if err == nil {
		t.Fatal("expected error for non-OIDC exec plugin")
	}
}

func TestParseOIDCExecPlugin_OpenIDScopeAutoAdded(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "kubectl",
		Args: []string{
			"oidc-login", "get-token",
			"--oidc-issuer-url=https://example.com",
			"--oidc-client-id=cid",
			"--oidc-extra-scope=profile",
		},
	}
	cfg, err := parseOIDCExecPlugin(exec)
	if err != nil {
		t.Fatalf("parseOIDCExecPlugin: %v", err)
	}
	if cfg.Scopes[0] != "openid" {
		t.Errorf("first scope should be openid, got %v", cfg.Scopes)
	}
}

func TestParseOIDCExecPlugin_OpenIDNotDuplicated(t *testing.T) {
	exec := &clientcmdapi.ExecConfig{
		Command: "kubectl",
		Args: []string{
			"oidc-login", "get-token",
			"--oidc-issuer-url=https://example.com",
			"--oidc-client-id=cid",
			"--oidc-extra-scope=openid",
			"--oidc-extra-scope=offline_access",
		},
	}
	cfg, err := parseOIDCExecPlugin(exec)
	if err != nil {
		t.Fatalf("parseOIDCExecPlugin: %v", err)
	}
	count := 0
	for _, s := range cfg.Scopes {
		if s == "openid" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("openid should appear exactly once, got %d in %v", count, cfg.Scopes)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
