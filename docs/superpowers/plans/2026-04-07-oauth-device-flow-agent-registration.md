# OAuth Device Flow Agent Registration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the one-time code agent registration flow with OAuth 2.0 Device Authorization Grant via Ory Hydra, including workspace selection in the consent step.

**Architecture:** CLI initiates Device Flow against Hydra, user authenticates and selects workspace in browser (agentserver serves as Hydra login/consent provider), CLI polls for token then registers agent with agentserver API using the OAuth access_token. Two-level token hierarchy: OAuth tokens (workspace-scoped) for auth, sandbox credentials (agent-scoped) for ongoing communication.

**Tech Stack:** Go 1.26, Ory Hydra v2 (external), `github.com/pkg/browser`, `github.com/mdp/qrterminal/v3`, React/TypeScript frontend, chi router, PostgreSQL.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/auth/hydra.go` | Hydra Admin API client — login/consent/introspection |
| `internal/auth/hydra_test.go` | Tests for Hydra client (HTTP mocking) |
| `internal/server/oauth_provider.go` | Login + Consent provider HTTP handlers |
| `internal/agent/login.go` | CLI `login` subcommand — Device Flow + registration |
| `internal/agent/credentials.go` | `~/.agentserver/.credentials.json` read/write |
| `internal/agent/credentials_test.go` | Tests for credentials storage |
| `internal/agent/token_refresh.go` | Three-tier credential refresh logic |
| `internal/agent/token_refresh_test.go` | Tests for token refresh |
| `web/src/components/OAuthConsent.tsx` | Consent page with workspace selection |
| `web/src/components/OAuthLogin.tsx` | Login page for Hydra login flow |

### Modified Files

| File | Changes |
|------|---------|
| `go.mod` / `go.sum` | Add `github.com/pkg/browser`, `github.com/mdp/qrterminal/v3` |
| `cmd/agentserver-agent/main.go` | Add `login` command; remove `--code` from `connect`/`claudecode`; add `--skip-open-browser` |
| `internal/server/server.go` | Add `HydraClient` field to Server struct; register `/oauth/*` routes in Router() |
| `internal/server/agent_register.go` | Rewrite `handleAgentRegister` from one-time-code auth to Bearer token + Hydra introspection |
| `internal/agent/connect.go` | Replace `Register()` call with `RunLogin()` call |
| `internal/agent/claudecode.go` | Replace `Register()` call with `RunLogin()` call |
| `internal/agent/client.go` | Remove old `Register()` function |
| `cmd/serve.go` | Initialize HydraClient from env vars, pass to Server |
| `web/src/App.tsx` | Add route for `/oauth/consent` and `/oauth/login` |
| `web/src/lib/api.ts` | Add `listUserWorkspaces()` API function |

---

### Task 1: Add Go dependencies

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add browser and QR code libraries**

```bash
cd /root/agentserver && go get github.com/pkg/browser github.com/mdp/qrterminal/v3
```

- [ ] **Step 2: Verify dependencies resolve**

Run: `cd /root/agentserver && go mod tidy`
Expected: clean exit, no errors.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add browser and qrterminal dependencies for Device Flow"
```

---

### Task 2: Credentials file storage (`internal/agent/credentials.go`)

**Files:**
- Create: `internal/agent/credentials.go`
- Create: `internal/agent/credentials_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/credentials_test.go`:

```go
package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := &Credentials{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC),
		HydraURL:     "https://auth.example.com",
		Scopes:       []string{"openid", "profile", "agent:register"},
	}

	if err := SaveCredentials(path, creds); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	loaded, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}

	if loaded.AccessToken != creds.AccessToken {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, creds.AccessToken)
	}
	if loaded.RefreshToken != creds.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, creds.RefreshToken)
	}
	if !loaded.ExpiresAt.Equal(creds.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", loaded.ExpiresAt, creds.ExpiresAt)
	}
	if loaded.HydraURL != creds.HydraURL {
		t.Errorf("HydraURL = %q, want %q", loaded.HydraURL, creds.HydraURL)
	}
}

func TestLoadCredentials_NotExist(t *testing.T) {
	creds, err := LoadCredentials("/nonexistent/.credentials.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil credentials for missing file, got %+v", creds)
	}
}

func TestDefaultCredentialsPath(t *testing.T) {
	path := DefaultCredentialsPath()
	if filepath.Base(path) != ".credentials.json" {
		t.Errorf("expected .credentials.json, got %s", filepath.Base(path))
	}
}

func TestSaveCredentials_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "deep", ".credentials.json")

	creds := &Credentials{
		AccessToken:  "tok",
		RefreshToken: "ref",
		ExpiresAt:    time.Now(),
		HydraURL:     "https://auth.example.com",
	}

	if err := SaveCredentials(nested, creds); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	// Verify file permissions are 0600.
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/agentserver && go test ./internal/agent/ -run TestSaveAndLoadCredentials -v`
Expected: FAIL — `Credentials` type undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/credentials.go`:

```go
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials holds OAuth tokens for the authenticated user.
// Stored in ~/.agentserver/.credentials.json.
type Credentials struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	HydraURL     string    `json:"hydraUrl"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// DefaultCredentialsPath returns ~/.agentserver/.credentials.json.
func DefaultCredentialsPath() string {
	return filepath.Join(DefaultRegistryDir(), ".credentials.json")
}

// LoadCredentials reads credentials from disk. Returns nil if the file does not exist.
func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return &creds, nil
}

// SaveCredentials writes credentials to disk with 0600 permissions.
func SaveCredentials(path string, creds *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /root/agentserver && go test ./internal/agent/ -run TestSaveAndLoad\|TestLoadCredentials_NotExist\|TestDefaultCredentialsPath\|TestSaveCredentials_CreatesDir -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/credentials.go internal/agent/credentials_test.go
git commit -m "feat: add credentials file storage for OAuth tokens"
```

---

### Task 3: Hydra Admin API client (`internal/auth/hydra.go`)

**Files:**
- Create: `internal/auth/hydra.go`
- Create: `internal/auth/hydra_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/auth/hydra_test.go`:

```go
package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHydraClient_GetLoginRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/oauth2/auth/requests/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("login_challenge") != "test-challenge" {
			t.Errorf("missing login_challenge param")
		}
		json.NewEncoder(w).Encode(LoginRequest{
			Challenge:      "test-challenge",
			Subject:        "user-123",
			Skip:           true,
			RequestedScope: []string{"openid", "profile"},
		})
	}))
	defer srv.Close()

	client := NewHydraClient(srv.URL, "https://public.example.com")
	req, err := client.GetLoginRequest("test-challenge")
	if err != nil {
		t.Fatalf("GetLoginRequest: %v", err)
	}
	if req.Challenge != "test-challenge" {
		t.Errorf("Challenge = %q, want %q", req.Challenge, "test-challenge")
	}
	if req.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", req.Subject, "user-123")
	}
	if !req.Skip {
		t.Error("expected Skip=true")
	}
}

func TestHydraClient_AcceptLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/admin/oauth2/auth/requests/login/accept" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body AcceptLoginBody
		json.NewDecoder(r.Body).Decode(&body)
		if body.Subject != "user-123" {
			t.Errorf("Subject = %q, want %q", body.Subject, "user-123")
		}
		json.NewEncoder(w).Encode(RedirectResponse{RedirectTo: "https://hydra/callback"})
	}))
	defer srv.Close()

	client := NewHydraClient(srv.URL, "https://public.example.com")
	redirect, err := client.AcceptLogin("test-challenge", AcceptLoginBody{
		Subject:  "user-123",
		Remember: true,
	})
	if err != nil {
		t.Fatalf("AcceptLogin: %v", err)
	}
	if redirect != "https://hydra/callback" {
		t.Errorf("redirect = %q, want %q", redirect, "https://hydra/callback")
	}
}

func TestHydraClient_GetConsentRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/oauth2/auth/requests/consent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ConsentRequest{
			Challenge:      "consent-challenge",
			Subject:        "user-123",
			RequestedScope: []string{"openid", "agent:register"},
		})
	}))
	defer srv.Close()

	client := NewHydraClient(srv.URL, "https://public.example.com")
	req, err := client.GetConsentRequest("consent-challenge")
	if err != nil {
		t.Fatalf("GetConsentRequest: %v", err)
	}
	if req.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", req.Subject, "user-123")
	}
}

func TestHydraClient_AcceptConsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var body AcceptConsentBody
		json.NewDecoder(r.Body).Decode(&body)
		wsID, _ := body.Session.AccessToken["workspace_id"].(string)
		if wsID != "ws-001" {
			t.Errorf("workspace_id = %q, want %q", wsID, "ws-001")
		}
		json.NewEncoder(w).Encode(RedirectResponse{RedirectTo: "https://hydra/done"})
	}))
	defer srv.Close()

	client := NewHydraClient(srv.URL, "https://public.example.com")
	redirect, err := client.AcceptConsent("consent-challenge", AcceptConsentBody{
		GrantScope: []string{"openid", "agent:register"},
		Session: ConsentSession{
			AccessToken: map[string]interface{}{
				"workspace_id":   "ws-001",
				"workspace_role": "developer",
			},
		},
	})
	if err != nil {
		t.Fatalf("AcceptConsent: %v", err)
	}
	if redirect != "https://hydra/done" {
		t.Errorf("redirect = %q", redirect)
	}
}

func TestHydraClient_IntrospectToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/admin/oauth2/introspect" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		r.ParseForm()
		if r.PostForm.Get("token") != "test-token" {
			t.Errorf("missing token in form")
		}
		json.NewEncoder(w).Encode(IntrospectionResult{
			Active:  true,
			Subject: "user-123",
			Scope:   "openid profile agent:register",
			Extra: map[string]interface{}{
				"workspace_id": "ws-001",
			},
		})
	}))
	defer srv.Close()

	client := NewHydraClient(srv.URL, "https://public.example.com")
	result, err := client.IntrospectToken("test-token")
	if err != nil {
		t.Fatalf("IntrospectToken: %v", err)
	}
	if !result.Active {
		t.Error("expected Active=true")
	}
	if result.Subject != "user-123" {
		t.Errorf("Subject = %q", result.Subject)
	}
	wsID, _ := result.Extra["workspace_id"].(string)
	if wsID != "ws-001" {
		t.Errorf("workspace_id = %q", wsID)
	}
}

func TestHydraClient_RejectLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(RedirectResponse{RedirectTo: "https://hydra/reject"})
	}))
	defer srv.Close()

	client := NewHydraClient(srv.URL, "https://public.example.com")
	redirect, err := client.RejectLogin("challenge", RejectBody{Error: "access_denied", ErrorDescription: "user cancelled"})
	if err != nil {
		t.Fatalf("RejectLogin: %v", err)
	}
	if redirect != "https://hydra/reject" {
		t.Errorf("redirect = %q", redirect)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/agentserver && go test ./internal/auth/ -run TestHydraClient -v`
Expected: FAIL — types undefined.

- [ ] **Step 3: Write implementation**

Create `internal/auth/hydra.go`:

```go
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HydraClient talks to the Ory Hydra Admin API.
type HydraClient struct {
	AdminURL  string // e.g. "http://hydra:4445"
	PublicURL string // e.g. "https://auth.example.com"
	client    *http.Client
}

// NewHydraClient creates a client for the given Hydra Admin URL.
func NewHydraClient(adminURL, publicURL string) *HydraClient {
	return &HydraClient{
		AdminURL:  strings.TrimRight(adminURL, "/"),
		PublicURL: strings.TrimRight(publicURL, "/"),
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// --- Types ---

type LoginRequest struct {
	Challenge      string   `json:"challenge"`
	Subject        string   `json:"subject"`
	Skip           bool     `json:"skip"`
	RequestedScope []string `json:"requested_scope"`
	Client         struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
}

type AcceptLoginBody struct {
	Subject     string `json:"subject"`
	Remember    bool   `json:"remember"`
	RememberFor int    `json:"remember_for,omitempty"`
}

type ConsentRequest struct {
	Challenge      string   `json:"challenge"`
	Subject        string   `json:"subject"`
	RequestedScope []string `json:"requested_scope"`
	Client         struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
}

type ConsentSession struct {
	AccessToken map[string]interface{} `json:"access_token,omitempty"`
	IDToken     map[string]interface{} `json:"id_token,omitempty"`
}

type AcceptConsentBody struct {
	GrantScope  []string       `json:"grant_scope"`
	Session     ConsentSession `json:"session"`
	Remember    bool           `json:"remember,omitempty"`
	RememberFor int            `json:"remember_for,omitempty"`
}

type RejectBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

type RedirectResponse struct {
	RedirectTo string `json:"redirect_to"`
}

type IntrospectionResult struct {
	Active   bool                   `json:"active"`
	Subject  string                 `json:"sub"`
	Scope    string                 `json:"scope"`
	ClientID string                 `json:"client_id"`
	Extra    map[string]interface{} `json:"ext"`
}

// --- Login Provider API ---

func (h *HydraClient) GetLoginRequest(challenge string) (*LoginRequest, error) {
	u := h.AdminURL + "/admin/oauth2/auth/requests/login?login_challenge=" + url.QueryEscape(challenge)
	resp, err := h.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("get login request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get login request: status %d: %s", resp.StatusCode, body)
	}
	var req LoginRequest
	if err := json.NewDecoder(resp.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("decode login request: %w", err)
	}
	return &req, nil
}

func (h *HydraClient) AcceptLogin(challenge string, body AcceptLoginBody) (string, error) {
	return h.putJSON("/admin/oauth2/auth/requests/login/accept", "login_challenge", challenge, body)
}

func (h *HydraClient) RejectLogin(challenge string, body RejectBody) (string, error) {
	return h.putJSON("/admin/oauth2/auth/requests/login/reject", "login_challenge", challenge, body)
}

// --- Consent Provider API ---

func (h *HydraClient) GetConsentRequest(challenge string) (*ConsentRequest, error) {
	u := h.AdminURL + "/admin/oauth2/auth/requests/consent?consent_challenge=" + url.QueryEscape(challenge)
	resp, err := h.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("get consent request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get consent request: status %d: %s", resp.StatusCode, body)
	}
	var req ConsentRequest
	if err := json.NewDecoder(resp.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("decode consent request: %w", err)
	}
	return &req, nil
}

func (h *HydraClient) AcceptConsent(challenge string, body AcceptConsentBody) (string, error) {
	return h.putJSON("/admin/oauth2/auth/requests/consent/accept", "consent_challenge", challenge, body)
}

func (h *HydraClient) RejectConsent(challenge string, body RejectBody) (string, error) {
	return h.putJSON("/admin/oauth2/auth/requests/consent/reject", "consent_challenge", challenge, body)
}

// --- Token Introspection ---

func (h *HydraClient) IntrospectToken(token string) (*IntrospectionResult, error) {
	form := url.Values{"token": {token}}
	resp, err := h.client.PostForm(h.AdminURL+"/admin/oauth2/introspect", form)
	if err != nil {
		return nil, fmt.Errorf("introspect token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("introspect token: status %d: %s", resp.StatusCode, body)
	}
	var result IntrospectionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode introspection: %w", err)
	}
	return &result, nil
}

// HasScope checks if the introspection result includes the given scope.
func (r *IntrospectionResult) HasScope(scope string) bool {
	for _, s := range strings.Split(r.Scope, " ") {
		if s == scope {
			return true
		}
	}
	return false
}

// --- Helpers ---

func (h *HydraClient) putJSON(path, queryKey, queryVal string, body interface{}) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal body: %w", err)
	}
	u := h.AdminURL + path + "?" + queryKey + "=" + url.QueryEscape(queryVal)
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("put request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("put %s: status %d: %s", path, resp.StatusCode, respBody)
	}
	var rr RedirectResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return "", fmt.Errorf("decode redirect: %w", err)
	}
	return rr.RedirectTo, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /root/agentserver && go test ./internal/auth/ -run TestHydraClient -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/hydra.go internal/auth/hydra_test.go
git commit -m "feat: add Hydra Admin API client for login/consent/introspection"
```

---

### Task 4: Server-side OAuth provider endpoints (`internal/server/oauth_provider.go`)

**Files:**
- Create: `internal/server/oauth_provider.go`
- Modify: `internal/server/server.go` (add `HydraClient` field + routes)
- Modify: `cmd/serve.go` (initialize `HydraClient` from env)

- [ ] **Step 1: Add HydraClient field to Server struct**

In `internal/server/server.go`, add the field to the `Server` struct (after the `ModelserverProxyURL` field around line 63):

```go
// Hydra OAuth2 (for agent Device Flow)
HydraClient *auth.HydraClient
```

- [ ] **Step 2: Register OAuth provider routes in Router()**

In `internal/server/server.go`, add routes in the `Router()` function. Insert after the agent registration line (after `r.Post("/api/agent/register", s.handleAgentRegister)` at line ~165), add:

```go
// Hydra login/consent provider endpoints (no auth required — Hydra redirects here).
if s.HydraClient != nil {
	r.Get("/oauth/login", s.handleOAuthLogin)
	r.Post("/oauth/login", s.handleOAuthLoginSubmit)
	r.Get("/oauth/consent", s.handleOAuthConsent)
	r.Post("/oauth/consent", s.handleOAuthConsentSubmit)
}
```

- [ ] **Step 3: Write OAuth provider handlers**

Create `internal/server/oauth_provider.go`:

```go
package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/agentserver/agentserver/internal/auth"
)

// handleOAuthLogin is the Hydra login provider endpoint.
// Hydra redirects here with ?login_challenge=xxx.
func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		http.Error(w, "missing login_challenge", http.StatusBadRequest)
		return
	}

	loginReq, err := s.HydraClient.GetLoginRequest(challenge)
	if err != nil {
		log.Printf("oauth login: get login request: %v", err)
		http.Error(w, "failed to get login request", http.StatusInternalServerError)
		return
	}

	// If Hydra says we can skip (user already authenticated), accept immediately.
	if loginReq.Skip {
		redirect, err := s.HydraClient.AcceptLogin(challenge, auth.AcceptLoginBody{
			Subject:  loginReq.Subject,
			Remember: true,
		})
		if err != nil {
			log.Printf("oauth login: accept skip: %v", err)
			http.Error(w, "failed to accept login", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}

	// Check if the user has an existing agentserver session cookie.
	userID := s.Auth.UserIDFromCookie(r)
	if userID != "" {
		redirect, err := s.HydraClient.AcceptLogin(challenge, auth.AcceptLoginBody{
			Subject:  userID,
			Remember: true,
		})
		if err != nil {
			log.Printf("oauth login: accept with cookie: %v", err)
			http.Error(w, "failed to accept login", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}

	// No session — redirect to the login page with the challenge as a query param.
	// The frontend will render a login form that POSTs to /oauth/login.
	http.Redirect(w, r, "/?oauth_login_challenge="+challenge, http.StatusFound)
}

// handleOAuthLoginSubmit processes the login form submission during OAuth flow.
// Called from the frontend after user authenticates via password or OIDC.
func (s *Server) handleOAuthLoginSubmit(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		http.Error(w, "missing login_challenge", http.StatusBadRequest)
		return
	}

	// The user should have logged in via the normal auth flow at this point.
	// The frontend calls this endpoint after setting the auth cookie.
	userID := s.Auth.UserIDFromCookie(r)
	if userID == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	redirect, err := s.HydraClient.AcceptLogin(challenge, auth.AcceptLoginBody{
		Subject:  userID,
		Remember: true,
	})
	if err != nil {
		log.Printf("oauth login submit: accept: %v", err)
		http.Error(w, "failed to accept login", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"redirect_to": redirect})
}

// handleOAuthConsent is the Hydra consent provider endpoint.
// Hydra redirects here with ?consent_challenge=xxx.
func (s *Server) handleOAuthConsent(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("consent_challenge")
	if challenge == "" {
		http.Error(w, "missing consent_challenge", http.StatusBadRequest)
		return
	}

	consentReq, err := s.HydraClient.GetConsentRequest(challenge)
	if err != nil {
		log.Printf("oauth consent: get consent request: %v", err)
		http.Error(w, "failed to get consent request", http.StatusInternalServerError)
		return
	}

	// Fetch the user's workspaces.
	workspaces, err := s.DB.ListWorkspacesByUser(consentReq.Subject)
	if err != nil {
		log.Printf("oauth consent: list workspaces: %v", err)
		http.Error(w, "failed to list workspaces", http.StatusInternalServerError)
		return
	}

	// Redirect to the consent UI with challenge as query param.
	// The frontend renders workspace selection.
	http.Redirect(w, r, "/?oauth_consent_challenge="+challenge, http.StatusFound)
}

// handleOAuthConsentSubmit processes the consent form submission (workspace selection).
func (s *Server) handleOAuthConsentSubmit(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("consent_challenge")
	if challenge == "" {
		http.Error(w, "missing consent_challenge", http.StatusBadRequest)
		return
	}

	var req struct {
		WorkspaceID string `json:"workspace_id"`
		Action      string `json:"action"` // "accept" or "deny"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.Action == "deny" {
		redirect, err := s.HydraClient.RejectConsent(challenge, auth.RejectBody{
			Error:            "access_denied",
			ErrorDescription: "user denied consent",
		})
		if err != nil {
			log.Printf("oauth consent: reject: %v", err)
			http.Error(w, "failed to reject consent", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"redirect_to": redirect})
		return
	}

	// Get consent request to extract subject.
	consentReq, err := s.HydraClient.GetConsentRequest(challenge)
	if err != nil {
		log.Printf("oauth consent submit: get consent request: %v", err)
		http.Error(w, "failed to get consent request", http.StatusInternalServerError)
		return
	}

	// Verify the user is a developer+ member of the selected workspace.
	role, err := s.DB.GetWorkspaceMemberRole(req.WorkspaceID, consentReq.Subject)
	if err != nil {
		log.Printf("oauth consent submit: check role: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if role == "" || role == "guest" {
		http.Error(w, "insufficient permissions for this workspace", http.StatusForbidden)
		return
	}

	redirect, err := s.HydraClient.AcceptConsent(challenge, auth.AcceptConsentBody{
		GrantScope: consentReq.RequestedScope,
		Session: auth.ConsentSession{
			AccessToken: map[string]interface{}{
				"workspace_id":   req.WorkspaceID,
				"workspace_role": role,
			},
			IDToken: map[string]interface{}{
				"workspace_id": req.WorkspaceID,
			},
		},
	})
	if err != nil {
		log.Printf("oauth consent submit: accept: %v", err)
		http.Error(w, "failed to accept consent", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"redirect_to": redirect})
}
```

- [ ] **Step 4: Add `UserIDFromCookie` to auth package**

The OAuth login handler needs to check if a user is already logged in without going through middleware. In `internal/auth/auth.go`, find the existing `Middleware` function and add a helper (check current patterns first—if the Auth struct already exposes a way to do this, use it instead). If not:

Add to `internal/auth/auth.go`:

```go
// UserIDFromCookie extracts the user ID from the request cookie without
// returning an error (returns "" if no valid session).
func (a *Auth) UserIDFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	userID, err := a.db.GetUserIDByToken(cookie.Value)
	if err != nil {
		return ""
	}
	return userID
}
```

Check if `cookieName` and `GetUserIDByToken` already exist — the existing `Middleware` function should already use them.

- [ ] **Step 5: Initialize HydraClient in `cmd/serve.go`**

In `cmd/serve.go`, after `srv.ModelserverProxyURL = ...` (around line 225), add:

```go
// Hydra OAuth2 for agent Device Flow.
hydraAdminURL := os.Getenv("HYDRA_ADMIN_URL")
hydraPublicURL := os.Getenv("HYDRA_PUBLIC_URL")
if hydraAdminURL != "" && hydraPublicURL != "" {
	srv.HydraClient = auth.NewHydraClient(hydraAdminURL, hydraPublicURL)
	log.Printf("Hydra OAuth2: admin=%s public=%s", hydraAdminURL, hydraPublicURL)
}
```

- [ ] **Step 6: Run the build to verify compilation**

Run: `cd /root/agentserver && go build ./...`
Expected: clean build, no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/server/oauth_provider.go internal/server/server.go internal/auth/auth.go cmd/serve.go
git commit -m "feat: add Hydra login/consent provider endpoints for OAuth Device Flow"
```

---

### Task 5: Rewrite agent registration API to use Bearer token (`internal/server/agent_register.go`)

**Files:**
- Modify: `internal/server/agent_register.go`

- [ ] **Step 1: Rewrite `handleAgentRegister` to use Bearer token + Hydra introspection**

Replace the entire `handleAgentRegister` function in `internal/server/agent_register.go`:

```go
// handleAgentRegister processes a CLI agent registration using an OAuth Bearer token.
// The token must contain workspace_id and agent:register scope from the Hydra consent flow.
func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	// Extract Bearer token.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Introspect token via Hydra.
	if s.HydraClient == nil {
		http.Error(w, "OAuth not configured", http.StatusServiceUnavailable)
		return
	}
	introspection, err := s.HydraClient.IntrospectToken(token)
	if err != nil {
		log.Printf("agent register: introspect token: %v", err)
		http.Error(w, "token introspection failed", http.StatusInternalServerError)
		return
	}
	if !introspection.Active {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	if !introspection.HasScope("agent:register") {
		http.Error(w, "insufficient scope: agent:register required", http.StatusForbidden)
		return
	}

	// Extract workspace_id from token claims.
	workspaceID, _ := introspection.Extra["workspace_id"].(string)
	if workspaceID == "" {
		http.Error(w, "token missing workspace_id claim", http.StatusBadRequest)
		return
	}
	userID := introspection.Subject

	// Verify workspace membership (defense in depth).
	role, err := s.DB.GetWorkspaceMemberRole(workspaceID, userID)
	if err != nil {
		log.Printf("agent register: check role: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if role == "" || role == "guest" {
		http.Error(w, "no permission to register agent in this workspace", http.StatusForbidden)
		return
	}

	// Parse request body.
	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "Local Agent"
	}
	sandboxType := req.Type
	if sandboxType == "" {
		sandboxType = "opencode"
	}
	if sandboxType != "opencode" && sandboxType != "claudecode" {
		http.Error(w, "invalid type: must be opencode or claudecode", http.StatusBadRequest)
		return
	}

	// Create sandbox (reuse existing logic).
	sandboxID := uuid.New().String()
	tunnelToken := generatePassword()
	proxyToken := generatePassword()
	var opencodePassword string
	if sandboxType == "opencode" {
		opencodePassword = generatePassword()
	}

	sid := shortid.Generate()
	var createErr error
	for attempts := 0; attempts < 3; attempts++ {
		createErr = s.DB.CreateLocalSandbox(sandboxID, workspaceID, req.Name, sandboxType, opencodePassword, proxyToken, tunnelToken, sid)
		if createErr == nil {
			break
		}
		sid = shortid.Generate()
	}
	if createErr != nil {
		log.Printf("agent register: create sandbox: %v", createErr)
		http.Error(w, "failed to register agent", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"sandbox_id":   sandboxID,
		"tunnel_token": tunnelToken,
		"proxy_token":  proxyToken,
		"workspace_id": workspaceID,
		"short_id":     sid,
	})
}
```

- [ ] **Step 2: Add `strings` import**

Make sure `"strings"` is in the import block of `agent_register.go`. Remove unused imports (`"time"`, `"crypto/rand"`, `"encoding/hex"` if `handleCreateAgentCode` is also removed in this file). Keep `handleCreateAgentCode` for now but it will be removed in Task 8.

- [ ] **Step 3: Run build to verify**

Run: `cd /root/agentserver && go build ./...`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/server/agent_register.go
git commit -m "feat: rewrite agent registration to use OAuth Bearer token + Hydra introspection"
```

---

### Task 6: CLI `login` subcommand (`internal/agent/login.go`)

**Files:**
- Create: `internal/agent/login.go`
- Modify: `cmd/agentserver-agent/main.go`

- [ ] **Step 1: Create `internal/agent/login.go`**

```go
package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/pkg/browser"
)

const defaultClientID = "agentserver-agent-cli"
const defaultScopes = "openid profile agent:register"

// LoginOptions holds flags for the login command.
type LoginOptions struct {
	ServerURL       string
	HydraPublicURL  string
	Name            string
	Type            string // "opencode" or "claudecode"
	SkipOpenBrowser bool
}

// DeviceAuthResponse is the response from Hydra's device authorization endpoint.
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse is the response from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// RegisterResponse is the response from the agent registration endpoint.
type RegisterResponse struct {
	SandboxID   string `json:"sandbox_id"`
	TunnelToken string `json:"tunnel_token"`
	ProxyToken  string `json:"proxy_token"`
	WorkspaceID string `json:"workspace_id"`
	ShortID     string `json:"short_id"`
}

// RunLogin executes the OAuth Device Flow login and agent registration.
func RunLogin(opts LoginOptions) error {
	if opts.ServerURL == "" {
		return fmt.Errorf("--server is required")
	}
	if opts.HydraPublicURL == "" {
		return fmt.Errorf("--hydra-url is required")
	}
	if opts.Name == "" {
		hostname, _ := os.Hostname()
		if hostname != "" {
			opts.Name = hostname
		} else {
			opts.Name = "Local Agent"
		}
	}
	if opts.Type == "" {
		opts.Type = "claudecode"
	}

	// 1. Request device authorization.
	deviceResp, err := requestDeviceCode(opts.HydraPublicURL)
	if err != nil {
		return fmt.Errorf("device authorization failed: %w", err)
	}

	// 2. Display authentication info.
	verifyURL := deviceResp.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = deviceResp.VerificationURI
	}
	fmt.Printf("\nTo authenticate, visit:\n  %s\n\n", verifyURL)
	if deviceResp.UserCode != "" {
		fmt.Printf("Or enter code: %s at %s\n\n", deviceResp.UserCode, deviceResp.VerificationURI)
	}

	// 3. Try opening browser; fall back to QR code.
	if !opts.SkipOpenBrowser {
		if err := browser.OpenURL(verifyURL); err != nil {
			log.Printf("Could not open browser: %v", err)
			showQRCode(verifyURL)
		}
	} else {
		showQRCode(verifyURL)
	}

	// 4. Poll for token.
	fmt.Println("Waiting for authentication...")
	tokenResp, err := pollForToken(opts.HydraPublicURL, deviceResp)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	fmt.Println("Authentication successful!")

	// 5. Register agent with access_token.
	regResp, err := registerAgentWithToken(opts.ServerURL, tokenResp.AccessToken, opts.Name, opts.Type)
	if err != nil {
		return fmt.Errorf("agent registration failed: %w", err)
	}

	// 6. Save credentials.
	credPath := DefaultCredentialsPath()
	if err := SaveCredentials(credPath, &Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		HydraURL:     opts.HydraPublicURL,
		Scopes:       strings.Split(tokenResp.Scope, " "),
	}); err != nil {
		log.Printf("Warning: failed to save credentials: %v", err)
	}

	// 7. Save registry entry.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	locked, err := LockRegistry(DefaultRegistryPath())
	if err != nil {
		return fmt.Errorf("lock registry: %w", err)
	}
	defer locked.Close()

	entry := &RegistryEntry{
		Dir:         cwd,
		Server:      opts.ServerURL,
		SandboxID:   regResp.SandboxID,
		TunnelToken: regResp.TunnelToken,
		WorkspaceID: regResp.WorkspaceID,
		Name:        opts.Name,
		Type:        opts.Type,
	}
	locked.Reg.Put(entry)
	if err := locked.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	fmt.Printf("Registered as '%s' in workspace '%s' (sandbox: %s)\n",
		opts.Name, regResp.WorkspaceID, regResp.SandboxID)
	return nil
}

func requestDeviceCode(hydraPublicURL string) (*DeviceAuthResponse, error) {
	form := url.Values{
		"client_id": {defaultClientID},
		"scope":     {defaultScopes},
	}
	resp, err := http.PostForm(strings.TrimRight(hydraPublicURL, "/")+"/oauth2/device/auth", form)
	if err != nil {
		return nil, fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device auth failed (%d): %s", resp.StatusCode, body)
	}
	var result DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device auth response: %w", err)
	}
	return &result, nil
}

func pollForToken(hydraPublicURL string, deviceResp *DeviceAuthResponse) (*TokenResponse, error) {
	tokenURL := strings.TrimRight(hydraPublicURL, "/") + "/oauth2/token"
	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("authorization expired, please try again")
		}

		time.Sleep(interval)

		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {defaultClientID},
			"device_code": {deviceResp.DeviceCode},
		}
		resp, err := http.PostForm(tokenURL, form)
		if err != nil {
			continue // Retry on network errors.
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var tokenResp TokenResponse
			if err := json.Unmarshal(body, &tokenResp); err != nil {
				return nil, fmt.Errorf("decode token response: %w", err)
			}
			return &tokenResp, nil
		}

		// Parse error response.
		var errResp struct {
			Error string `json:"error"`
		}
		json.Unmarshal(body, &errResp)

		switch errResp.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return nil, fmt.Errorf("authorization denied by user")
		case "expired_token":
			return nil, fmt.Errorf("authorization expired, please try again")
		default:
			return nil, fmt.Errorf("token error: %s", errResp.Error)
		}
	}
}

func registerAgentWithToken(serverURL, accessToken, name, agentType string) (*RegisterResponse, error) {
	reqBody := fmt.Sprintf(`{"name":%q,"type":%q}`, name, agentType)
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(serverURL, "/")+"/api/agent/register",
		strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed (%d): %s", resp.StatusCode, body)
	}

	var result RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	return &result, nil
}

func showQRCode(url string) {
	config := qrterminal.Config{
		Level:     qrterminal.L,
		Writer:    os.Stderr,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 1,
	}
	qrterminal.GenerateWithConfig(url, config)
}
```

- [ ] **Step 2: Add `login` command to `cmd/agentserver-agent/main.go`**

Add new variables and command. After the existing variable declarations (around line 31):

```go
var (
	hydraURL        string
	skipOpenBrowser bool
)
```

Add the login command definition (before or after `claudecodeCmd`):

```go
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate and register this agent with agentserver",
	Long: `Authenticate with agentserver using OAuth Device Flow and register this
machine as a local agent in a workspace of your choice.

Opens a browser for authentication. If browser is unavailable, displays a URL
and QR code for manual login.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := agent.RunLogin(agent.LoginOptions{
			ServerURL:       server,
			HydraPublicURL:  hydraURL,
			Name:            name,
			Type:            "claudecode",
			SkipOpenBrowser: skipOpenBrowser,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}
```

In `init()`, register the command and flags. Replace the current `init()` function body:

```go
func init() {
	rootCmd.AddCommand(connectCmd, claudecodeCmd, loginCmd, listCmd, removeCmd, taskWorkerCmd, mcpServerCmd, versionCmd)

	// login command flags.
	loginCmd.Flags().StringVar(&server, "server", "", "Agent server URL (e.g., https://cli.example.com)")
	loginCmd.Flags().StringVar(&hydraURL, "hydra-url", "", "Hydra public URL (e.g., https://auth.example.com)")
	loginCmd.Flags().StringVar(&name, "name", "", "Name for this agent (default: hostname)")
	loginCmd.Flags().BoolVar(&skipOpenBrowser, "skip-open-browser", false, "Don't auto-open browser, show URL + QR only")

	// connect command flags (remove --code, add --skip-open-browser).
	connectCmd.Flags().StringVar(&server, "server", "", "Agent server URL (e.g., https://cli.example.com)")
	connectCmd.Flags().StringVar(&hydraURL, "hydra-url", "", "Hydra public URL for OAuth login")
	connectCmd.Flags().StringVar(&name, "name", "", "Name for this agent (default: hostname)")
	connectCmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID to connect to")
	connectCmd.Flags().StringVar(&opencodeURL, "opencode-url", "", "Local opencode server URL (default: http://localhost:{opencode-port})")
	connectCmd.Flags().StringVar(&opencodeToken, "opencode-token", "", "Local opencode server token")
	connectCmd.Flags().BoolVar(&autoStart, "auto-start", true, "Automatically start opencode serve")
	connectCmd.Flags().StringVar(&opencodeBin, "opencode-bin", "opencode", "Path to the opencode binary")
	connectCmd.Flags().IntVar(&opencodePort, "opencode-port", 4096, "Port to start opencode on")
	connectCmd.Flags().BoolVar(&skipOpenBrowser, "skip-open-browser", false, "Don't auto-open browser, show URL + QR only")

	// claudecode command flags (remove --code, add --hydra-url and --skip-open-browser).
	claudecodeCmd.Flags().StringVar(&server, "server", "", "Agent server URL (e.g., https://cli.example.com)")
	claudecodeCmd.Flags().StringVar(&hydraURL, "hydra-url", "", "Hydra public URL for OAuth login")
	claudecodeCmd.Flags().StringVar(&name, "name", "", "Name for this agent (default: hostname)")
	claudecodeCmd.Flags().StringVar(&claudeBin, "claude-bin", "claude", "Path to the claude binary")
	claudecodeCmd.Flags().StringVar(&claudeWorkDir, "work-dir", "", "Working directory for Claude Code (default: current directory)")
	claudecodeCmd.Flags().BoolVar(&skipOpenBrowser, "skip-open-browser", false, "Don't auto-open browser, show URL + QR only")

	taskWorkerCmd.Flags().String("server", "", "Agent server URL")
	taskWorkerCmd.Flags().String("proxy-token", "", "Sandbox proxy token")
	taskWorkerCmd.Flags().String("sandbox-id", "", "Sandbox ID")
	taskWorkerCmd.Flags().String("work-dir", "", "Working directory for task execution (default: current)")
	taskWorkerCmd.Flags().String("claude-bin", "claude", "Path to the claude binary")

	removeCmd.Flags().String("workspace", "", "Workspace ID of the agent to remove")
	removeCmd.Flags().String("dir", "", "Directory of the agent to remove (default: current directory)")
	removeCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
}
```

- [ ] **Step 3: Run build to verify**

Run: `cd /root/agentserver && go build ./cmd/agentserver-agent/`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/login.go cmd/agentserver-agent/main.go
git commit -m "feat: add CLI login command with OAuth Device Flow, browser + QR code"
```

---

### Task 7: Token refresh logic (`internal/agent/token_refresh.go`)

**Files:**
- Create: `internal/agent/token_refresh.go`
- Create: `internal/agent/token_refresh_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/token_refresh_test.go`:

```go
package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshAccessToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
		}
		if r.PostForm.Get("refresh_token") != "old-refresh" {
			t.Errorf("refresh_token = %q", r.PostForm.Get("refresh_token"))
		}
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	resp, err := refreshAccessToken(srv.URL, "old-refresh")
	if err != nil {
		t.Fatalf("refreshAccessToken: %v", err)
	}
	if resp.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q", resp.AccessToken)
	}
	if resp.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q", resp.RefreshToken)
	}
}

func TestRefreshAccessToken_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	defer srv.Close()

	_, err := refreshAccessToken(srv.URL, "bad-refresh")
	if err == nil {
		t.Fatal("expected error for invalid refresh token")
	}
}

func TestEnsureValidCredentials_AlreadyValid(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")
	regPath := filepath.Join(dir, "registry.json")

	// Save valid credentials (won't actually be used since ping succeeds).
	SaveCredentials(credPath, &Credentials{
		AccessToken:  "tok",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(time.Hour),
		HydraURL:     "https://auth.example.com",
	})

	entry := &RegistryEntry{
		Server:      "https://server.example.com",
		SandboxID:   "sandbox-1",
		TunnelToken: "tunnel-tok",
	}

	// pingFunc succeeds — credentials are valid.
	err := ensureValidCredentials(entry, credPath, regPath, func(e *RegistryEntry) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/agentserver && go test ./internal/agent/ -run TestRefreshAccessToken\|TestEnsureValidCredentials -v`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Write implementation**

Create `internal/agent/token_refresh.go`:

```go
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNeedReLogin indicates all tokens have expired and interactive re-auth is needed.
var ErrNeedReLogin = errors.New("all tokens expired, please run 'agentserver-agent login' again")

// EnsureValidCredentials checks sandbox credentials and refreshes if needed.
// Uses the default credential and registry paths.
func EnsureValidCredentials(entry *RegistryEntry) error {
	return ensureValidCredentials(entry, DefaultCredentialsPath(), DefaultRegistryPath(), pingServer)
}

// ensureValidCredentials is the testable inner implementation.
func ensureValidCredentials(entry *RegistryEntry, credPath, regPath string, pingFn func(*RegistryEntry) error) error {
	// 1. Try existing sandbox credentials.
	if err := pingFn(entry); err == nil {
		return nil
	}

	// 2. Load OAuth credentials.
	creds, err := LoadCredentials(credPath)
	if err != nil || creds == nil {
		return ErrNeedReLogin
	}

	// 3. Try refresh_token.
	if creds.RefreshToken == "" {
		return ErrNeedReLogin
	}

	newToken, err := refreshAccessToken(creds.HydraURL, creds.RefreshToken)
	if err != nil {
		return ErrNeedReLogin
	}

	// 4. Re-register with new access_token.
	regResp, err := registerAgentWithToken(entry.Server, newToken.AccessToken, entry.Name, entry.Type)
	if err != nil {
		return ErrNeedReLogin
	}

	// 5. Update entry and save.
	entry.SandboxID = regResp.SandboxID
	entry.TunnelToken = regResp.TunnelToken

	locked, lockErr := LockRegistry(regPath)
	if lockErr == nil {
		locked.Reg.Put(entry)
		locked.Save()
		locked.Close()
	}

	// 6. Update credentials.
	creds.AccessToken = newToken.AccessToken
	creds.RefreshToken = newToken.RefreshToken
	creds.ExpiresAt = time.Now().Add(time.Duration(newToken.ExpiresIn) * time.Second)
	SaveCredentials(credPath, creds)

	return nil
}

func refreshAccessToken(hydraPublicURL, refreshToken string) (*TokenResponse, error) {
	tokenURL := strings.TrimRight(hydraPublicURL, "/") + "/oauth2/token"
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {defaultClientID},
		"refresh_token": {refreshToken},
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh token failed (%d): %s", resp.StatusCode, body)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	return &tokenResp, nil
}

// pingServer verifies sandbox credentials are still valid.
func pingServer(entry *RegistryEntry) error {
	req, err := http.NewRequest(http.MethodGet,
		strings.TrimRight(entry.Server, "/")+"/api/agent/tasks/poll?sandbox_id="+entry.SandboxID,
		nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+entry.TunnelToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("credentials expired (status %d)", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /root/agentserver && go test ./internal/agent/ -run TestRefreshAccessToken\|TestEnsureValidCredentials -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/token_refresh.go internal/agent/token_refresh_test.go
git commit -m "feat: add three-tier credential refresh logic for agent tokens"
```

---

### Task 8: Update `connect` and `claudecode` to use login flow

**Files:**
- Modify: `internal/agent/connect.go`
- Modify: `internal/agent/claudecode.go`
- Modify: `internal/agent/client.go` (remove old `Register` function)

- [ ] **Step 1: Update `ConnectOptions` and `RunConnect` in `connect.go`**

In `internal/agent/connect.go`, replace the `ConnectOptions` struct and update `RunConnect`:

Replace `ConnectOptions`:

```go
type ConnectOptions struct {
	Server          string
	HydraURL        string
	Name            string
	WorkspaceID     string
	SkipOpenBrowser bool
	OpencodeURL     string
	OpencodeURLSet  bool
	OpencodeToken   string
	AutoStart       bool
	OpencodeBin     string
	OpencodePort    int
	OpencodePortSet bool
}
```

In `RunConnect`, replace the `if opts.Code != ""` block (lines 48-88) with:

```go
	// Check if we need to register first (no saved credentials for this directory).
	entries := reg.FindByDir(cwd)
	if len(entries) == 0 {
		// --- New registration via OAuth Device Flow ---
		if opts.Server == "" {
			log.Fatal("--server is required for registration")
		}
		if opts.HydraURL == "" {
			log.Fatal("--hydra-url is required for registration")
		}
		locked.Close() // Release lock during interactive login.

		if err := RunLogin(LoginOptions{
			ServerURL:       opts.Server,
			HydraPublicURL:  opts.HydraURL,
			Name:            opts.Name,
			Type:            "opencode",
			SkipOpenBrowser: opts.SkipOpenBrowser,
		}); err != nil {
			log.Fatalf("Login failed: %v", err)
		}

		// Re-lock and reload registry.
		locked, err = LockRegistry(registryPath)
		if err != nil {
			log.Fatalf("Failed to reload registry: %v", err)
		}
		defer locked.Close()
		reg = locked.Reg
		entries = reg.FindByDir(cwd)
		if len(entries) == 0 {
			log.Fatal("Registration succeeded but no entry found in registry")
		}
	}
```

Then replace the `else` reconnect block (lines 89-114) with:

```go
	// Select entry.
	var entry *RegistryEntry
	switch len(entries) {
	case 1:
		entry = entries[0]
	default:
		if opts.WorkspaceID == "" {
			log.Printf("Multiple workspaces registered for this directory:")
			for _, e := range entries {
				log.Printf("  workspace=%s  name=%s  sandbox=%s", e.WorkspaceID, e.Name, e.SandboxID)
			}
			log.Fatal("Use --workspace to specify which one to connect.")
		}
		entry = reg.Find(cwd, opts.WorkspaceID)
		if entry == nil {
			log.Fatalf("No entry found for workspace %q in this directory", opts.WorkspaceID)
		}
	}
	log.Printf("Using credentials (sandbox: %s)", entry.SandboxID)
	if opts.Server != "" {
		entry.Server = opts.Server
	}
```

- [ ] **Step 2: Update `ClaudeCodeOptions` and `RunClaudeCode` in `claudecode.go`**

Replace `ClaudeCodeOptions`:

```go
type ClaudeCodeOptions struct {
	Server          string
	HydraURL        string
	Name            string
	SkipOpenBrowser bool
	ClaudeBin       string
	WorkDir         string
}
```

In `RunClaudeCode`, replace the `if opts.Code != ""` block (lines 48-78) with:

```go
	entries := reg.FindByDir(cwd)
	// Filter to claudecode entries only.
	var ccEntries []*RegistryEntry
	for _, e := range entries {
		if e.Type == "claudecode" {
			ccEntries = append(ccEntries, e)
		}
	}

	if len(ccEntries) == 0 {
		// --- New registration via OAuth Device Flow ---
		if opts.Server == "" {
			log.Fatal("--server is required for registration")
		}
		if opts.HydraURL == "" {
			log.Fatal("--hydra-url is required for registration")
		}
		locked.Close()

		if err := RunLogin(LoginOptions{
			ServerURL:       opts.Server,
			HydraPublicURL:  opts.HydraURL,
			Name:            opts.Name,
			Type:            "claudecode",
			SkipOpenBrowser: opts.SkipOpenBrowser,
		}); err != nil {
			log.Fatalf("Login failed: %v", err)
		}

		locked, err = LockRegistry(registryPath)
		if err != nil {
			log.Fatalf("Failed to reload registry: %v", err)
		}
		defer locked.Close()
		reg = locked.Reg
		ccEntries = nil
		for _, e := range reg.FindByDir(cwd) {
			if e.Type == "claudecode" {
				ccEntries = append(ccEntries, e)
			}
		}
		if len(ccEntries) == 0 {
			log.Fatal("Registration succeeded but no claudecode entry found")
		}
	}

	var entry *RegistryEntry
	switch len(ccEntries) {
	case 1:
		entry = ccEntries[0]
	default:
		log.Printf("Multiple Claude Code agents registered for this directory:")
		for _, e := range ccEntries {
			log.Printf("  workspace=%s  name=%s  sandbox=%s", e.WorkspaceID, e.Name, e.SandboxID)
		}
		log.Fatal("Use 'remove' to clean up duplicates.")
	}
```

Remove the `else` block that was lines 79-105.

- [ ] **Step 3: Remove old `Register` function from `client.go`**

In `internal/agent/client.go`, delete the `Register` function (lines 56-93). The function is fully replaced by `RunLogin` + `registerAgentWithToken`.

- [ ] **Step 4: Update the `connect` command in `cmd/agentserver-agent/main.go`**

Update the `connectCmd.Run` function to pass the new fields:

```go
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect local opencode to agentserver",
	Long: `Establish a WebSocket tunnel between a local opencode instance and agentserver.

On first run, provide --server and --hydra-url to authenticate and register.
On subsequent runs, the saved credentials will be used automatically.

By default, opencode serve is started automatically on --opencode-port (4096).
Use --auto-start=false to disable this and manage opencode manually.`,
	Run: func(cmd *cobra.Command, args []string) {
		agent.RunConnect(agent.ConnectOptions{
			Server:          server,
			HydraURL:        hydraURL,
			Name:            name,
			WorkspaceID:     workspaceID,
			SkipOpenBrowser: skipOpenBrowser,
			OpencodeURL:     opencodeURL,
			OpencodeURLSet:  cmd.Flags().Changed("opencode-url"),
			OpencodeToken:   opencodeToken,
			AutoStart:       autoStart,
			OpencodeBin:     opencodeBin,
			OpencodePort:    opencodePort,
			OpencodePortSet: cmd.Flags().Changed("opencode-port"),
		})
	},
}
```

Update `claudecodeCmd` similarly:

```go
var claudecodeCmd = &cobra.Command{
	Use:   "claudecode",
	Short: "Connect local Claude Code terminal to agentserver",
	Long: `Register a local Claude Code instance with agentserver and expose its terminal
via WebSocket tunnel. Users can access the terminal through the web browser at
claude-{id}.{domain}.

On first run, provide --server and --hydra-url to authenticate and register.
On subsequent runs, saved credentials are used automatically.`,
	Run: func(cmd *cobra.Command, args []string) {
		agent.RunClaudeCode(agent.ClaudeCodeOptions{
			Server:          server,
			HydraURL:        hydraURL,
			Name:            name,
			SkipOpenBrowser: skipOpenBrowser,
			ClaudeBin:       claudeBin,
			WorkDir:         claudeWorkDir,
		})
	},
}
```

- [ ] **Step 5: Remove the `code` variable and `--code` flags**

In `cmd/agentserver-agent/main.go`, remove the `code` variable from the top-level `var` block (line 21). The `--code` flags were already removed in the `init()` rewrite in Task 6.

- [ ] **Step 6: Run build to verify**

Run: `cd /root/agentserver && go build ./cmd/agentserver-agent/`
Expected: clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/connect.go internal/agent/claudecode.go internal/agent/client.go cmd/agentserver-agent/main.go
git commit -m "feat: migrate connect/claudecode commands from one-time code to OAuth Device Flow"
```

---

### Task 9: Remove old one-time code endpoint

**Files:**
- Modify: `internal/server/agent_register.go` (remove `handleCreateAgentCode`)
- Modify: `internal/server/server.go` (remove route)

- [ ] **Step 1: Remove `handleCreateAgentCode` from `agent_register.go`**

Delete the entire `handleCreateAgentCode` function from `internal/server/agent_register.go`. Also remove any unused imports (`"crypto/rand"`, `"encoding/hex"`, `"time"`) that were only used by that function. Keep imports needed by the rewritten `handleAgentRegister`.

- [ ] **Step 2: Remove the route from `server.go`**

In `internal/server/server.go`, in the `Router()` method, remove:

```go
// Agent registration code generation
r.Post("/api/workspaces/{wid}/agent-code", s.handleCreateAgentCode)
```

(around line 279)

- [ ] **Step 3: Run build to verify**

Run: `cd /root/agentserver && go build ./...`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/server/agent_register.go internal/server/server.go
git commit -m "refactor: remove one-time code agent registration endpoint"
```

---

### Task 10: Frontend consent and login pages

**Files:**
- Create: `web/src/components/OAuthConsent.tsx`
- Create: `web/src/components/OAuthLogin.tsx`
- Modify: `web/src/lib/api.ts` (add API functions)
- Modify: `web/src/App.tsx` (add routes)

- [ ] **Step 1: Add API functions in `web/src/lib/api.ts`**

Add to the end of the file:

```typescript
export async function listMyWorkspaces(): Promise<Workspace[]> {
  const res = await fetch('/api/workspaces', { credentials: 'include' })
  if (!res.ok) throw new Error('Failed to list workspaces')
  return res.json()
}

export async function submitOAuthLogin(loginChallenge: string): Promise<{ redirect_to: string }> {
  const res = await fetch(`/oauth/login?login_challenge=${encodeURIComponent(loginChallenge)}`, {
    method: 'POST',
    credentials: 'include',
  })
  if (!res.ok) throw new Error('Failed to submit login')
  return res.json()
}

export async function submitOAuthConsent(
  consentChallenge: string,
  workspaceId: string,
  action: 'accept' | 'deny'
): Promise<{ redirect_to: string }> {
  const res = await fetch(`/oauth/consent?consent_challenge=${encodeURIComponent(consentChallenge)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ workspace_id: workspaceId, action }),
  })
  if (!res.ok) throw new Error('Failed to submit consent')
  return res.json()
}
```

- [ ] **Step 2: Create `OAuthLogin.tsx`**

Create `web/src/components/OAuthLogin.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { Login } from './Login'
import { submitOAuthLogin } from '../lib/api'

interface OAuthLoginProps {
  challenge: string
}

export function OAuthLogin({ challenge }: OAuthLoginProps) {
  const [error, setError] = useState('')

  const handleLoginSuccess = async () => {
    try {
      const { redirect_to } = await submitOAuthLogin(challenge)
      window.location.href = redirect_to
    } catch (err) {
      setError('Failed to complete OAuth login. Please try again.')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="w-full max-w-md space-y-4">
        <div className="text-center mb-6">
          <h2 className="text-lg font-semibold">Sign in to authorize agent</h2>
          <p className="text-sm text-[var(--muted-foreground)]">
            An agent is requesting access to your account
          </p>
        </div>
        {error && (
          <div className="text-sm text-red-500 text-center">{error}</div>
        )}
        <Login onSuccess={handleLoginSuccess} />
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Create `OAuthConsent.tsx`**

Create `web/src/components/OAuthConsent.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { listMyWorkspaces, submitOAuthConsent, type Workspace } from '../lib/api'

interface OAuthConsentProps {
  challenge: string
}

export function OAuthConsent({ challenge }: OAuthConsentProps) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [selected, setSelected] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    listMyWorkspaces()
      .then((ws) => {
        setWorkspaces(ws)
        if (ws.length === 1) setSelected(ws[0].id)
        setLoading(false)
      })
      .catch(() => {
        setError('Failed to load workspaces')
        setLoading(false)
      })
  }, [])

  const handleSubmit = async (action: 'accept' | 'deny') => {
    if (action === 'accept' && !selected) {
      setError('Please select a workspace')
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const { redirect_to } = await submitOAuthConsent(challenge, selected, action)
      window.location.href = redirect_to
    } catch {
      setError('Failed to submit. Please try again.')
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-[var(--muted-foreground)]">Loading...</div>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="w-full max-w-md border border-[var(--border)] rounded-lg p-6 space-y-6">
        <div className="text-center">
          <h2 className="text-lg font-semibold">Agent requests access</h2>
          <p className="text-sm text-[var(--muted-foreground)] mt-1">
            Select a workspace for the agent to join
          </p>
        </div>

        {workspaces.length === 0 ? (
          <div className="text-center text-sm text-[var(--muted-foreground)]">
            No workspaces available. Contact your administrator.
          </div>
        ) : (
          <div className="space-y-2">
            {workspaces.map((ws) => (
              <label
                key={ws.id}
                className={`flex items-center gap-3 p-3 rounded-md border cursor-pointer transition-colors ${
                  selected === ws.id
                    ? 'border-[var(--primary)] bg-[var(--primary)]/5'
                    : 'border-[var(--border)] hover:border-[var(--muted-foreground)]'
                }`}
              >
                <input
                  type="radio"
                  name="workspace"
                  value={ws.id}
                  checked={selected === ws.id}
                  onChange={() => setSelected(ws.id)}
                  className="accent-[var(--primary)]"
                />
                <span className="text-sm font-medium">{ws.name}</span>
              </label>
            ))}
          </div>
        )}

        <div className="space-y-2 text-sm text-[var(--muted-foreground)]">
          <p className="font-medium text-[var(--foreground)]">Permissions requested:</p>
          <ul className="space-y-1 ml-2">
            <li>Register as local agent</li>
            <li>Receive and execute tasks</li>
          </ul>
        </div>

        {error && (
          <div className="text-sm text-red-500 text-center">{error}</div>
        )}

        <div className="flex gap-3 justify-end">
          <button
            onClick={() => handleSubmit('deny')}
            disabled={submitting}
            className="px-4 py-2 text-sm border border-[var(--border)] rounded-md hover:bg-[var(--muted)]"
          >
            Deny
          </button>
          <button
            onClick={() => handleSubmit('accept')}
            disabled={submitting || !selected}
            className="px-4 py-2 text-sm bg-[var(--primary)] text-[var(--primary-foreground)] rounded-md hover:opacity-90 disabled:opacity-50"
          >
            {submitting ? 'Authorizing...' : 'Allow & Join'}
          </button>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Add OAuth routes to `App.tsx`**

In `web/src/App.tsx`, add imports at the top:

```tsx
import { OAuthConsent } from './components/OAuthConsent'
import { OAuthLogin } from './components/OAuthLogin'
```

In the `App` component, at the beginning of the function body (before the existing `useEffect` hooks), add:

```tsx
  // Check for OAuth flow query params.
  const searchParams = new URLSearchParams(location.search)
  const oauthLoginChallenge = searchParams.get('oauth_login_challenge')
  const oauthConsentChallenge = searchParams.get('oauth_consent_challenge')

  // Render OAuth pages if we're in an OAuth flow.
  if (oauthLoginChallenge) {
    return <OAuthLogin challenge={oauthLoginChallenge} />
  }
  if (oauthConsentChallenge) {
    return <OAuthConsent challenge={oauthConsentChallenge} />
  }
```

- [ ] **Step 5: Verify frontend builds**

Run: `cd /root/agentserver/web && npm run build`
Expected: clean build (or `npx tsc --noEmit` for type checking).

- [ ] **Step 6: Commit**

```bash
git add web/src/components/OAuthConsent.tsx web/src/components/OAuthLogin.tsx web/src/lib/api.ts web/src/App.tsx
git commit -m "feat: add OAuth consent and login pages for workspace selection"
```

---

### Task 11: Integration verification

**Files:** None — verification only.

- [ ] **Step 1: Full Go build**

Run: `cd /root/agentserver && go build ./...`
Expected: clean build.

- [ ] **Step 2: Run all Go tests**

Run: `cd /root/agentserver && go test ./internal/agent/ ./internal/auth/ -v`
Expected: all tests pass.

- [ ] **Step 3: Verify CLI help**

Run: `cd /root/agentserver && go run ./cmd/agentserver-agent/ login --help`
Expected: shows login command with `--server`, `--hydra-url`, `--name`, `--skip-open-browser` flags.

Run: `cd /root/agentserver && go run ./cmd/agentserver-agent/ connect --help`
Expected: shows connect command without `--code` flag, with `--hydra-url` and `--skip-open-browser` flags.

- [ ] **Step 4: Verify frontend build**

Run: `cd /root/agentserver/web && npm run build`
Expected: clean build.

- [ ] **Step 5: Commit all remaining changes (if any unstaged)**

```bash
cd /root/agentserver && git status
# Stage and commit any remaining files if needed.
```
