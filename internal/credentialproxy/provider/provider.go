package provider

import "net/http"

// Provider handles credential injection for a single external service kind.
type Provider interface {
	// Kind returns the short identifier used in URL paths and DB rows.
	Kind() string

	// ParseUpload validates a user-uploaded config blob and returns the fields to persist.
	// AuthSecret will be AES-GCM encrypted before storage.
	ParseUpload(contentType string, raw []byte) (*UploadResult, error)

	// BuildSandboxConfig produces config artifacts to mount into a sandbox at provisioning time.
	// Pure function: no DB, no network.
	BuildSandboxConfig(
		bindings []*BindingMeta,
		proxyToken string,
		credproxyPublicURL string,
	) ([]*SandboxConfigFile, error)

	// ServeHTTP handles a proxied request from a sandbox.
	// The caller has already authenticated and decrypted the binding.
	ServeHTTP(w http.ResponseWriter, r *http.Request, b *DecryptedBinding)
}

// UploadResult is returned by ParseUpload with fields to persist.
type UploadResult struct {
	DisplayName       string
	ServerURL         string
	PublicMeta        map[string]any
	AuthType          string
	AuthSecret        []byte // plaintext; will be encrypted before storage
	PendingDeviceAuth bool   // true = requires device code flow before credential is usable
}

// BindingMeta contains non-secret fields of a credential binding.
type BindingMeta struct {
	ID          string
	WorkspaceID string
	Kind        string
	DisplayName string
	ServerURL   string
	PublicMeta  map[string]any
	AuthType    string
	IsDefault   bool
}

// DecryptedBinding is a binding with its auth secret decrypted.
type DecryptedBinding struct {
	BindingMeta
	AuthSecret []byte // plaintext; provider interprets per its own format
}

// SandboxConfigFile is a file to mount into a sandbox pod.
type SandboxConfigFile struct {
	// Relative path under /var/run/agentserver/. No "..".
	SubPath string
	Mode    int32
	Content []byte

	// Env vars to set on the sandbox pod.
	EnvVars map[string]string
}
