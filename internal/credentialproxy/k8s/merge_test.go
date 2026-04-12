package k8s

import (
	"strings"
	"testing"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
	"k8s.io/client-go/tools/clientcmd"
)

func TestBuildMergedKubeconfig_Single(t *testing.T) {
	bindings := []*provider.BindingMeta{{
		ID:          "bind-abc123",
		DisplayName: "prod-cluster",
		IsDefault:   true,
	}}

	file, err := BuildMergedKubeconfig(bindings, "my-proxy-token", "http://credproxy:8083")
	if err != nil {
		t.Fatalf("BuildMergedKubeconfig: %v", err)
	}
	if file.SubPath != "kubeconfig" {
		t.Errorf("SubPath = %q, want %q", file.SubPath, "kubeconfig")
	}
	if file.EnvVars["KUBECONFIG"] != "/var/run/agentserver/kubeconfig" {
		t.Errorf("KUBECONFIG env = %q", file.EnvVars["KUBECONFIG"])
	}

	// Parse the generated kubeconfig to validate it.
	cfg, err := clientcmd.Load(file.Content)
	if err != nil {
		t.Fatalf("Load generated kubeconfig: %v", err)
	}
	if cfg.CurrentContext != "prod-cluster" {
		t.Errorf("current-context = %q, want %q", cfg.CurrentContext, "prod-cluster")
	}
	cluster := cfg.Clusters["prod-cluster"]
	if cluster == nil {
		t.Fatal("cluster not found")
	}
	if !strings.Contains(cluster.Server, "credproxy:8083/k8s/bind-abc123") {
		t.Errorf("cluster server = %q", cluster.Server)
	}
	user := cfg.AuthInfos["prod-cluster-user"]
	if user == nil {
		t.Fatal("user not found")
	}
	if user.Token != "my-proxy-token" {
		t.Errorf("user token = %q", user.Token)
	}
}

func TestBuildMergedKubeconfig_Multi(t *testing.T) {
	bindings := []*provider.BindingMeta{
		{ID: "bind-1", DisplayName: "staging", IsDefault: false},
		{ID: "bind-2", DisplayName: "prod", IsDefault: true},
	}

	file, err := BuildMergedKubeconfig(bindings, "tok", "http://cp:8083")
	if err != nil {
		t.Fatalf("BuildMergedKubeconfig: %v", err)
	}

	cfg, err := clientcmd.Load(file.Content)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("current-context = %q, want %q", cfg.CurrentContext, "prod")
	}
	if len(cfg.Clusters) != 2 {
		t.Errorf("got %d clusters, want 2", len(cfg.Clusters))
	}
	if len(cfg.Contexts) != 2 {
		t.Errorf("got %d contexts, want 2", len(cfg.Contexts))
	}
}

func TestBuildMergedKubeconfig_NoBindings(t *testing.T) {
	_, err := BuildMergedKubeconfig(nil, "tok", "http://cp:8083")
	if err == nil {
		t.Fatal("expected error for empty bindings")
	}
}

func TestBuildMergedKubeconfig_NoDefault(t *testing.T) {
	bindings := []*provider.BindingMeta{
		{ID: "bind-1", DisplayName: "staging", IsDefault: false},
	}
	_, err := BuildMergedKubeconfig(bindings, "tok", "http://cp:8083")
	if err == nil {
		t.Fatal("expected error for no default")
	}
}
