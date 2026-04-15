package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ValidateIssuerURL checks that an OIDC issuer URL is https and not a private IP.
// Exported so the agentserver handler can call it before OIDC discovery.
func ValidateIssuerURL(issuerURL string) error {
	u, err := url.Parse(issuerURL)
	if err != nil {
		return fmt.Errorf("invalid issuer URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("issuer URL must use https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("issuer URL %q points to a private IP address", issuerURL)
		}
	}
	return nil
}

// OIDCAuthConfig is the auth_blob plaintext format for auth_type="oidc".
type OIDCAuthConfig struct {
	IssuerURL    string   `json:"issuer_url"`
	ClientID     string   `json:"client_id"`
	Scopes       []string `json:"scopes"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	AccessToken  string   `json:"access_token,omitempty"`
	TokenExpiry  string   `json:"token_expiry,omitempty"`
}

// parseOIDCExecPlugin extracts OIDC parameters from a kubeconfig exec plugin
// that matches the kubelogin / oidc-login pattern.
func parseOIDCExecPlugin(exec *clientcmdapi.ExecConfig) (*OIDCAuthConfig, error) {
	args := exec.Args

	// Detect oidc-login pattern: args must contain "oidc-login" and "get-token".
	hasOIDCLogin := false
	hasGetToken := false
	for _, a := range args {
		if a == "oidc-login" {
			hasOIDCLogin = true
		}
		if a == "get-token" {
			hasGetToken = true
		}
	}
	if !hasOIDCLogin || !hasGetToken {
		return nil, fmt.Errorf("exec plugin is not an oidc-login plugin")
	}

	cfg := &OIDCAuthConfig{}
	var scopes []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, hasEq := parseFlag(arg)
		if key == "" {
			continue
		}
		// If --flag value (no =), consume next arg.
		if !hasEq && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			val = args[i+1]
			i++
		}

		switch key {
		case "--oidc-issuer-url":
			cfg.IssuerURL = val
		case "--oidc-client-id":
			cfg.ClientID = val
		case "--oidc-extra-scope":
			scopes = append(scopes, val)
		}
	}

	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("oidc-login exec plugin missing --oidc-issuer-url")
	}
	if err := ValidateIssuerURL(cfg.IssuerURL); err != nil {
		return nil, fmt.Errorf("oidc-login exec plugin: %w", err)
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc-login exec plugin missing --oidc-client-id")
	}

	// Ensure "openid" scope is present.
	hasOpenID := false
	for _, s := range scopes {
		if s == "openid" {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		scopes = append([]string{"openid"}, scopes...)
	}
	cfg.Scopes = scopes

	return cfg, nil
}

// parseFlag splits "--key=value" into (key, value, true) or "--key" into (key, "", false).
func parseFlag(arg string) (key, val string, hasEq bool) {
	if !strings.HasPrefix(arg, "--") {
		return "", "", false
	}
	if idx := strings.IndexByte(arg, '='); idx >= 0 {
		return arg[:idx], arg[idx+1:], true
	}
	return arg, "", false
}

// --- Token cache for credentialproxy runtime ---

var (
	tokenCache    = &oidcTokenCache{tokens: make(map[string]*cachedToken)}
	providerCache = &oidcProviderCache{providers: make(map[string]*gooidc.Provider)}
	refreshGroup  singleflight.Group
)

type cachedToken struct {
	accessToken  string
	refreshToken string
	expiry       time.Time
}

type oidcTokenCache struct {
	mu     sync.Mutex
	tokens map[string]*cachedToken
}

type oidcProviderCache struct {
	mu        sync.RWMutex
	providers map[string]*gooidc.Provider
}

func (c *oidcProviderCache) get(ctx context.Context, issuerURL string) (*gooidc.Provider, error) {
	c.mu.RLock()
	p, ok := c.providers[issuerURL]
	c.mu.RUnlock()
	if ok {
		return p, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check after acquiring write lock.
	if p, ok := c.providers[issuerURL]; ok {
		return p, nil
	}
	p, err := gooidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", issuerURL, err)
	}
	c.providers[issuerURL] = p
	return p, nil
}

// getOIDCBearerToken returns a valid access token for the given OIDC binding.
// It caches tokens in memory and refreshes them when expired.
func getOIDCBearerToken(ctx context.Context, bindingID string, authSecret []byte) (string, error) {
	// Check cache first.
	tokenCache.mu.Lock()
	cached, ok := tokenCache.tokens[bindingID]
	tokenCache.mu.Unlock()
	if ok && time.Now().Before(cached.expiry.Add(-30*time.Second)) {
		return cached.accessToken, nil
	}

	// Use singleflight to avoid concurrent refreshes for the same binding.
	// Re-read cache inside closure to avoid stale pointer from pre-closure capture.
	result, err, _ := refreshGroup.Do(bindingID, func() (interface{}, error) {
		tokenCache.mu.Lock()
		current := tokenCache.tokens[bindingID]
		tokenCache.mu.Unlock()
		return doOIDCRefresh(ctx, bindingID, authSecret, current)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func doOIDCRefresh(ctx context.Context, bindingID string, authSecret []byte, cached *cachedToken) (string, error) {
	var cfg OIDCAuthConfig
	if err := json.Unmarshal(authSecret, &cfg); err != nil {
		return "", fmt.Errorf("unmarshal oidc config: %w", err)
	}

	refreshToken := cfg.RefreshToken
	// If we have a cached refresh token (from rotation), prefer it.
	if cached != nil && cached.refreshToken != "" {
		refreshToken = cached.refreshToken
	}
	if refreshToken == "" {
		return "", fmt.Errorf("oidc binding %s has no refresh token", bindingID)
	}

	provider, err := providerCache.get(ctx, cfg.IssuerURL)
	if err != nil {
		return "", err
	}

	oauth2Cfg := oauth2.Config{
		ClientID: cfg.ClientID,
		Endpoint: provider.Endpoint(),
		Scopes:   cfg.Scopes,
	}

	// Create an expired token with the refresh token to trigger refresh.
	oldToken := &oauth2.Token{
		Expiry:       time.Now().Add(-time.Hour),
		RefreshToken: refreshToken,
	}

	newToken, err := oauth2Cfg.TokenSource(ctx, oldToken).Token()
	if err != nil {
		return "", fmt.Errorf("oidc token refresh for binding %s: %w", bindingID, err)
	}

	// Cache the new token.
	entry := &cachedToken{
		accessToken:  newToken.AccessToken,
		refreshToken: newToken.RefreshToken,
		expiry:       newToken.Expiry,
	}

	if newToken.RefreshToken != "" && newToken.RefreshToken != refreshToken {
		slog.Warn("oidc refresh token rotated; in-memory cache updated but DB still has original token",
			"binding_id", bindingID)
	}

	tokenCache.mu.Lock()
	tokenCache.tokens[bindingID] = entry
	tokenCache.mu.Unlock()

	return newToken.AccessToken, nil
}
