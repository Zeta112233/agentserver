package k8s

import (
	"fmt"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd"
)

// BuildMergedKubeconfig generates a merged kubeconfig that routes all cluster
// traffic through the credential proxy. Pure function: no I/O.
func BuildMergedKubeconfig(
	bindings []*provider.BindingMeta,
	proxyToken string,
	credproxyURL string,
) (*provider.SandboxConfigFile, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("no bindings to merge")
	}

	cfg := clientcmdapi.NewConfig()

	var defaultContext string
	for _, b := range bindings {
		clusterName := b.DisplayName
		userName := b.DisplayName + "-user"
		contextName := b.DisplayName

		cfg.Clusters[clusterName] = &clientcmdapi.Cluster{
			Server:                fmt.Sprintf("%s/k8s/%s", credproxyURL, b.ID),
			InsecureSkipTLSVerify: true,
		}

		cfg.AuthInfos[userName] = &clientcmdapi.AuthInfo{
			Token: proxyToken,
		}

		cfg.Contexts[contextName] = &clientcmdapi.Context{
			Cluster:  clusterName,
			AuthInfo: userName,
		}

		if b.IsDefault {
			defaultContext = contextName
		}
	}

	if defaultContext == "" {
		return nil, fmt.Errorf("no default binding found among %d bindings", len(bindings))
	}
	cfg.CurrentContext = defaultContext

	data, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, fmt.Errorf("serialize kubeconfig: %w", err)
	}

	return &provider.SandboxConfigFile{
		SubPath: "kubeconfig",
		Mode:    0o644,
		Content: data,
		EnvVars: map[string]string{
			"KUBECONFIG": "/var/run/agentserver/kubeconfig",
		},
	}, nil
}
