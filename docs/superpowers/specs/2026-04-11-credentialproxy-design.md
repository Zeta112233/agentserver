# Credential Proxy Design

**Date:** 2026-04-11
**Status:** Draft — pending review
**Depends on:** existing `llmproxy` service, `validate-proxy-token` internal endpoint, sandbox provisioning in `internal/sandbox/manager.go`

## Overview

Add a new sibling service, `credentialproxy`, that lets sandboxes access user-supplied external APIs without ever seeing the real credentials. v1 supports one backend — Kubernetes API servers — but the scaffolding is built around a generic `Provider` interface so GitHub/GitLab/etc. can be added in v2+ as pure additions.

The pattern is inspired by nanoclaw's Anthropic credential proxy (`/root/nanoclaw/src/credential-proxy.ts`): the sandbox speaks to a local proxy URL with a placeholder identity token; the proxy strips the placeholder, injects the real credential, and forwards to the upstream. Nanoclaw's code handles only Anthropic; this design generalizes the idea and extends it to k8s (including SPDY/WebSocket upgrades for `exec`/`port-forward`/`cp`).

## Goals

- Sandboxes can call user-supplied external k8s clusters (EKS, GKE, self-managed — anything with a standard apiserver) without ever seeing the cluster's real bearer token or client certificate.
- Full kubectl support: REST verbs, `--watch`, `exec`, `port-forward`, `attach`, `cp`, `proxy`.
- User-uploaded kubeconfigs with either static bearer token or inline client certificate + key.
- Credentials encrypted at rest with AES-GCM-256.
- Multi-cluster per workspace: a workspace can bind N kubeconfigs; sandboxes see a merged kubeconfig and switch with `kubectl config use-context`.
- SSRF-hardened at upload time and at dial time, including DNS rebinding.
- Extensibility scaffolding (`Provider` interface, `kind` column, `/{kind}/{id}/*` routing) so GitHub/GitLab providers can be added without touching v1 code.

## Non-goals (v1)

- GitHub and GitLab provider implementations (the interface is designed for them, the implementations are not shipped).
- kubeconfig `exec` plugin support. EKS and GKE native kubeconfigs require this; v1 requires users to create a dedicated ServiceAccount and upload a static-token kubeconfig instead.
- `auth-provider`, basic auth, or any kubeconfig auth type other than bearer token / client cert.
- Persistent audit log. v1 is structured slog to stderr only.
- mTLS between sandbox and credentialproxy. Intra-cluster HTTP is assumed trusted by the k8s network model and is what v1 uses.
- Encryption key rotation. Single static key via env var. Rotation requires an operator runbook (double-read window + re-encrypt migration), not a code feature.
- Hot reload of merged kubeconfig for running sandboxes. A kubeconfig added to a workspace only takes effect for newly-started sandboxes.
- Credential helper / short-lived token minting (relevant for a future `git` integration).

## Architecture

### Trust boundaries and request legs

```
 ┌──────────────┐                              ┌─────────────────┐
 │              │                              │ credentialproxy │
 │   sandbox    │         (1) intra-cluster    │                 │
 │              ├─ HTTP ─────────────────────▶ │  - encKey (env) │
 │ KUBECONFIG=  │  bearer=proxy_token          │  - direct DB    │
 │  merged yaml │                              │    connection   │
 │              │                              │  - local AES-   │
 │ server=      │                              │    GCM decrypt  │
 │  http://cp/  │                              │  - upgrade-     │
 │  k8s/{cid}   │                              │    aware        │
 └──────────────┘                              │    handler      │
                                               └────────┬────────┘
                                                        │
                                         (2) HTTPS      │
                                         TLS verified   │
                                         against CA,    │
                                         real creds     │
                                         injected       │
                                                        ▼
                                             ┌──────────────────────┐
                                             │ external k8s         │
                                             │ apiserver            │
                                             │ (user-supplied)      │
                                             └──────────────────────┘

 ┌──────────────┐                              ┌─────────────────┐
 │   User UI    │─ HTTP ─┐                     │  PostgreSQL     │
 │ upload/edit  │        │  (write path)       │  (shared)       │
 │  kubeconfig  │        ▼                     │                 │
 └──────────────┘  ┌──────────────┐  writes    │ credential_     │
                   │ agentserver  │───────────▶│  bindings       │
                   │ (control     │            │  (encrypted)    │
                   │  plane)      │            │                 │
                   │ encKey (env) │            │                 │
                   └──────────────┘            │                 │
                                    reads      │                 │
                                    ◀──────────┤                 │
                                               └─────────────────┘
                                                        ▲
                                                        │ (3) reads
                                                        │     encrypted
                                                        │     blob
                                               ┌────────┴────────┐
                                               │ credentialproxy │
                                               └─────────────────┘
```

**Three request legs:**

1. **sandbox → credentialproxy** — intra-cluster plain HTTP. The sandbox's existing `proxy_token` (the one it already uses for llmproxy) is the `Authorization: Bearer` value. The sandbox has no knowledge of real upstream credentials.

2. **credentialproxy → PostgreSQL** — direct SQL `SELECT` of `credential_bindings` rows. credentialproxy's DB role is scoped read-only to `credential_bindings` and `workspaces`. It decrypts the `auth_blob` locally using the shared `CREDPROXY_ENCRYPTION_KEY`.

3. **credentialproxy → external cluster** — HTTPS with TLS verification against the CA from the binding. Real credentials (bearer token or client cert) are injected via a provider-specific `http.Transport`. Handles both plain REST and upgrade (SPDY/WebSocket) via k8s's `UpgradeAwareHandler`.

### Trust model claims

- **Sandboxes never see real upstream credentials.** They only see their own `proxy_token` and the credentialproxy URL.
- **credentialproxy is stateless.** Process restart loses nothing. Source of truth is the DB.
- **encKey exists in two processes** (agentserver for write, credentialproxy for read). Both are deployed from the same k8s Secret.
- **credentialproxy RCE is the main blast radius.** An attacker with code execution in credentialproxy has both the encryption key and read access to the full `credential_bindings` table, so they can decrypt every workspace's bindings. This is the conscious trade-off vs. an HTTP-fetch-per-request architecture, made for latency and simplicity. Mitigations: minimal container (non-root, read-only rootfs, dropped capabilities), scoped DB role, NetworkPolicy.
- **DB leak without encKey is useless.** A raw DB dump (backup, snapshot, SQL injection read) yields only ciphertext.
- **Encryption key rotation is a runbook, not a feature.** v1 ships with a static key.

## Components

### New packages and files

#### Generic (per-provider-agnostic) in credentialproxy

| File | Purpose | Approx lines |
|---|---|---|
| `cmd/credentialproxy/main.go` | Entry point: load config, open DB, register providers, start HTTP server, graceful shutdown | ~70 |
| `internal/credentialproxy/config.go` | `LoadConfigFromEnv()` — port, DB URL, agentserver URL, encryption key, timeouts, SSRF override | ~60 |
| `internal/credentialproxy/server.go` | `NewServer()`, `Routes()`, route registration, provider dispatch middleware | ~120 |
| `internal/credentialproxy/store.go` | DAO for `credential_bindings` — `GetBinding(workspaceID, kind, bindingID)`, `ListBindings(workspaceID, kind)` | ~100 |
| `internal/credentialproxy/auth.go` | `ValidateProxyToken()` — calls agentserver's `/internal/validate-proxy-token` (mirrors `internal/llmproxy/auth.go`) | ~50 |
| `internal/credentialproxy/audit.go` | `LogProxied()`, `LogUpgradeOpen()`, `LogUpgradeClosed()` — structured slog helpers with fixed field shapes | ~80 |
| `internal/credentialproxy/provider/provider.go` | `Provider` interface + supporting types (`UploadResult`, `BindingMeta`, `DecryptedBinding`, `SandboxConfigFile`) | ~80 |
| `internal/credentialproxy/provider/registry.go` | `Register(kind, Provider)`, `Lookup(kind)`, `All()` | ~40 |

#### K8s provider (the only provider shipped in v1)

| File | Purpose | Approx lines |
|---|---|---|
| `internal/credentialproxy/k8s/k8s.go` | `K8sProvider{}` — implements `provider.Provider`; registers itself on init | ~150 |
| `internal/credentialproxy/k8s/parse.go` | `ParseKubeconfig()` — validate uploaded yaml, extract fields, reject unsupported auth types, SSRF upload-time check | ~180 |
| `internal/credentialproxy/k8s/merge.go` | `BuildMergedKubeconfig(bindings, proxyToken, credproxyURL)` — pure function, no I/O | ~100 |
| `internal/credentialproxy/k8s/transport.go` | `buildUpstreamTransport(binding)` — `*http.Transport` with CA pool, bearer or client-cert, SSRF dial-time guard | ~120 |
| `internal/credentialproxy/k8s/handler.go` | Per-request handling: strip path prefix, drop Authorization, call `UpgradeAwareHandler.ServeHTTP` | ~100 |
| `internal/credentialproxy/k8s/*_test.go` | Unit tests with fixture kubeconfigs under `testdata/` | ~500 |

#### Future provider placeholders

`internal/credentialproxy/github/` and `internal/credentialproxy/gitlab/` do not exist as directories in v1. They are called out here for future work; nothing is created.

#### Shared utilities

| File | Purpose | Approx lines |
|---|---|---|
| `internal/crypto/aesgcm.go` | `LoadKeyFromEnv()`, `Encrypt()`, `Decrypt()` — AES-GCM-256 with 12-byte nonce prefix | ~60 |
| `internal/crypto/aesgcm_test.go` | Round-trip, tamper detection, error cases | ~100 |

#### agentserver control plane additions

| File | Purpose | Approx lines |
|---|---|---|
| `internal/db/migrations/016_credential_bindings.sql` | DDL for the new table | ~30 |
| `internal/db/credential_bindings.go` | DAO for CRUD | ~180 |
| `internal/server/credential_bindings.go` | HTTP handlers (list/create/patch/delete/set-default), delegates to `provider.Registry.Lookup(kind).ParseUpload()` for validation | ~200 |
| `internal/server/credential_bindings_test.go` | Handler tests | ~250 |

#### agentserver modifications to existing files

| File | Change |
|---|---|
| `internal/sandbox/manager.go` | During sandbox creation, iterate `provider.Registry.All()`, collect `SandboxConfigFile` slices from each provider that has bindings for the workspace, merge into a single k8s Secret mounted at `/var/run/agentserver/`, aggregate env vars into the pod spec. |
| `internal/sandbox/config.go` | Declare that `KUBECONFIG` (and any other provider env vars) are valid sandbox env. |
| `cmd/serve.go` | Load `CREDPROXY_ENCRYPTION_KEY` and `CREDPROXY_PUBLIC_URL` at startup; fail-fast if missing when credentialproxy is enabled. Register providers on the agentserver side so CRUD handlers can validate uploads. |
| `docker-compose.yml` | Add `credentialproxy` service with `depends_on: [postgres]` and env vars. |
| `Dockerfile.credentialproxy` | New two-stage build mirroring `Dockerfile.llmproxy`. |

### DB schema

```sql
-- internal/db/migrations/016_credential_bindings.sql

CREATE TABLE credential_bindings (
    id            TEXT PRIMARY KEY,            -- 16-byte URL-safe base64, also used as {binding_id} in URLs
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,               -- 'k8s' in v1; future: 'github', 'gitlab', ...
    display_name  TEXT NOT NULL,               -- user-provided friendly name; used as kubeconfig context name
    server_url    TEXT NOT NULL,               -- canonical upstream URL (e.g., https://EKS.amazonaws.com)
    public_meta   JSONB NOT NULL DEFAULT '{}', -- non-sensitive kind-specific fields (e.g., base64 CA for k8s)
    auth_type     TEXT NOT NULL,               -- kind-specific: 'bearer' or 'client_cert' for k8s
    auth_blob     BYTEA NOT NULL,              -- AES-GCM ciphertext: nonce(12) || ciphertext || tag(16)
    is_default    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, kind, display_name)
);

CREATE INDEX idx_credential_bindings_ws_kind
    ON credential_bindings(workspace_id, kind);

-- Exactly one default per (workspace, kind)
CREATE UNIQUE INDEX idx_credential_bindings_one_default_per_kind
    ON credential_bindings(workspace_id, kind)
    WHERE is_default;
```

**`auth_blob` plaintext JSON formats (k8s provider):**

```json
// auth_type = "bearer"
{"token": "eyJhbGciOi..."}

// auth_type = "client_cert"
{"cert_pem": "-----BEGIN CERTIFICATE-----\n...", "key_pem": "-----BEGIN RSA PRIVATE KEY-----\n..."}
```

**`public_meta` for k8s provider:**

```json
{"ca_data_b64": "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t..."}
```

CA is stored in `public_meta` (not `auth_blob`) because a CA certificate is not sensitive. Other providers will define their own `public_meta` shape.

### Provider interface

```go
// internal/credentialproxy/provider/provider.go

package provider

import "net/http"

// Provider handles credential injection for a single external service kind.
// Each implementation is registered in the registry and dispatched by URL prefix.
type Provider interface {
    // Kind returns the short identifier used in URL paths and DB rows.
    // Must match ^[a-z][a-z0-9_-]*$. Examples: "k8s", "github", "gitlab".
    Kind() string

    // ParseUpload validates a user-uploaded config blob (e.g. kubeconfig yaml,
    // GitHub PAT + hostname, service account JSON) and returns the fields to
    // persist. Called from the agentserver CRUD handler.
    //
    // AuthSecret will be AES-GCM encrypted before storage. ServerURL and
    // PublicMeta are stored in plaintext.
    ParseUpload(contentType string, raw []byte) (*UploadResult, error)

    // BuildSandboxConfig produces the config artifacts to mount into a sandbox
    // at provisioning time. Pure function: no DB, no network. The input
    // `bindings` is a metadata-only slice; secrets are not included here
    // because sandbox config files should not contain real credentials.
    // `proxyToken` is baked into the generated config as the sandbox's
    // bearer identity to credentialproxy.
    //
    // Returns a slice so a single provider can emit multiple files
    // (e.g., GitHub may emit both gh/hosts.yml and .netrc).
    BuildSandboxConfig(
        bindings []*BindingMeta,
        proxyToken string,
        credproxyPublicURL string,
    ) ([]*SandboxConfigFile, error)

    // ServeHTTP handles a single request from a sandbox. The generic
    // middleware has already authenticated proxy_token, resolved the binding
    // via URL, and decrypted auth_blob. The provider only forwards with
    // injected credentials.
    ServeHTTP(w http.ResponseWriter, r *http.Request, b *DecryptedBinding)
}

type UploadResult struct {
    DisplayName string
    ServerURL   string
    PublicMeta  map[string]any
    AuthType    string
    AuthSecret  []byte
}

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

type DecryptedBinding struct {
    BindingMeta
    AuthSecret []byte // plaintext; provider interprets per its own format
}

type SandboxConfigFile struct {
    // Relative path under /var/run/agentserver/. No "..".
    // Examples: "kubeconfig", "gh/hosts.yml".
    SubPath string
    Mode    int32
    Content []byte

    // Env vars to set on the sandbox pod. Keys must be namespaced per
    // provider to prevent collisions (e.g., "KUBECONFIG", "GH_CONFIG_DIR").
    EnvVars map[string]string
}
```

### Environment variables

**agentserver (new):**

| Variable | Required | Description |
|---|---|---|
| `CREDPROXY_ENCRYPTION_KEY` | yes | 32-byte AES-256 key; accepts base64, hex, or raw. Fail-fast if invalid length. |
| `CREDPROXY_PUBLIC_URL` | yes | The URL sandboxes use to reach credentialproxy (e.g., `http://credentialproxy.agentserver.svc:8083`). Baked into merged kubeconfigs at provisioning time. |

**credentialproxy (all new):**

| Variable | Default | Description |
|---|---|---|
| `CREDPROXY_PORT` | `8083` | Listen port |
| `CREDPROXY_DATABASE_URL` | (required) | PostgreSQL URL — typically the same DB as agentserver, different role |
| `CREDPROXY_AGENTSERVER_URL` | (required) | e.g., `http://server:8080` — used for `/internal/validate-proxy-token` |
| `CREDPROXY_ENCRYPTION_KEY` | (required) | Must match agentserver's value exactly |
| `CREDPROXY_LOG_LEVEL` | `info` | slog level |
| `CREDPROXY_UPSTREAM_TIMEOUT` | `60s` | Non-upgrade request timeout. Upgrade connections are not time-limited (client decides) |
| `CREDPROXY_ALLOW_PRIVATE_UPSTREAMS` | `false` | When `true`, disable the SSRF private-IP guard. For internal testing only. Logged loudly on startup and reflected in `/readyz` response |

### HTTP routes

**credentialproxy:**

```
GET  /healthz                          → 200 ok (process alive)
GET  /readyz                           → 200 when DB ping succeeds
     /{kind}/{binding_id}/*            → generic middleware → provider dispatch
```

The `/{kind}/{binding_id}/*` pattern uses chi's wildcard to capture the upstream path. The generic middleware validates the proxy_token, looks up the binding filtering by `(workspace_id, kind, binding_id)`, decrypts `auth_blob`, and hands off to `provider.Registry.Lookup(kind).ServeHTTP`. `binding_id` is validated against `^[A-Za-z0-9_-]{16,64}$` as a defense-in-depth measure.

**agentserver (new):**

```
GET    /api/workspaces/{wsid}/credentials/{kind}
POST   /api/workspaces/{wsid}/credentials/{kind}              body: {display_name, config}
PATCH  /api/workspaces/{wsid}/credentials/{kind}/{id}
DELETE /api/workspaces/{wsid}/credentials/{kind}/{id}
POST   /api/workspaces/{wsid}/credentials/{kind}/{id}/set-default
```

All protected by the existing session cookie + workspace membership middleware. Response bodies never include `auth_blob`, decrypted values, or any secret material — only `id`, `display_name`, `server_url`, `auth_type`, `public_meta`, `is_default`, `created_at`.

**Default binding semantics:** `POST` creating the first binding for a `(workspace, kind)` pair implicitly sets `is_default = true`. `DELETE` on the current default binding returns `409 Conflict` with a message directing the caller to first `set-default` another binding, unless it is the last remaining binding in which case deletion is allowed (and leaves the workspace with zero bindings for that kind — the sandbox will simply not receive a kubeconfig). `set-default` on a non-default binding atomically flips `is_default` from the previous default to the new one in a single transaction to maintain the partial unique index invariant.

### Encryption helper API

```go
// internal/crypto/aesgcm.go

package crypto

// LoadKeyFromEnv reads a 32-byte AES-256 key from envVar.
// Accepts standard base64, URL-safe base64, hex, or raw 32 bytes.
// Returns an error if missing or wrong length after decoding.
func LoadKeyFromEnv(envVar string) ([]byte, error)

// Encrypt encrypts plaintext with AES-GCM-256. Output layout:
//     [12-byte nonce][ciphertext][16-byte GCM tag]
// The nonce is generated from crypto/rand per call.
func Encrypt(key, plaintext []byte) ([]byte, error)

// Decrypt reverses Encrypt. Returns an error if the tag check fails
// (tampered ciphertext, wrong key, or wrong format).
func Decrypt(key, ciphertext []byte) ([]byte, error)
```

### Sandbox provisioning changes

Pseudo-code for the new branch in `internal/sandbox/manager.go`:

```go
// After allocating sandbox row and proxy_token, before creating the pod:

var configFiles []*provider.SandboxConfigFile
for _, prov := range registry.All() {
    bindings, err := db.ListBindings(workspaceID, prov.Kind())
    if err != nil {
        return fmt.Errorf("list bindings for kind %s: %w", prov.Kind(), err)
    }
    if len(bindings) == 0 {
        continue
    }
    files, err := prov.BuildSandboxConfig(bindings, sbx.ProxyToken, cfg.CredproxyPublicURL)
    if err != nil {
        return fmt.Errorf("build sandbox config for kind %s: %w", prov.Kind(), err)
    }
    configFiles = append(configFiles, files...)
}

if len(configFiles) > 0 {
    // Create a single per-sandbox Secret named agent-sandbox-{short_id}-creds.
    // ownerReferences point at the Sandbox CR so the Secret is garbage collected
    // with the sandbox.
    // Each SandboxConfigFile becomes a key in Secret.data keyed by SubPath.
    createSecretWithFiles(ctx, secretName, configFiles)

    // Add volume + mount at /var/run/agentserver/ with items mapped to SubPaths.
    addVolumeAndMount(podSpec, secretName, "/var/run/agentserver")

    // Merge env vars from all providers into podSpec.env.
    // Collision detection: if two providers emit the same env var key, fail at
    // startup (this is a bug in the providers, not a runtime concern).
    addEnvVars(podSpec, configFiles)
}
```

If the workspace has no bindings for any provider, the Secret, mount, and env vars are simply absent. Tools inside the sandbox that need these files (e.g., `kubectl` with no `KUBECONFIG`) will error normally.

## Data flows

### Flow 1: Upload kubeconfig

1. Workspace owner pastes kubeconfig yaml into the UI, clicks "Save".
2. Browser `POST /api/workspaces/{wsid}/credentials/k8s` with `{display_name, config: "<yaml>"}`.
3. agentserver middleware verifies session cookie and workspace membership.
4. `credential_bindings.go` handler looks up `provider.Registry.Lookup("k8s")` and calls `K8sProvider.ParseUpload("application/yaml", raw)`.
5. `ParseUpload`:
   - Parse yaml via `k8s.io/client-go/tools/clientcmd`.
   - Require exactly one cluster / user / context, or select by `current-context` if multiple are present.
   - Extract `server_url` from `clusters[i].cluster.server`. Reject non-`https://`. Reject literal private IPs (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, ::1, fc00::/7) — SSRF layer 1.
   - Extract CA from `clusters[i].cluster.certificate-authority-data`. Reject `certificate-authority` (file path) because we can't follow paths into the user's filesystem.
   - Extract auth from `users[i].user`:
     - `token` → `auth_type = "bearer"`, `auth_secret = {"token": ...}`.
     - `client-certificate-data` + `client-key-data` → `auth_type = "client_cert"`, `auth_secret = {"cert_pem": ..., "key_pem": ...}`.
     - `exec` → reject with `400 exec plugin not supported; create a dedicated ServiceAccount and upload its static-token kubeconfig`.
     - `auth-provider`, `username`/`password` → reject 400 with specific message.
   - Validate CA is a parseable PEM certificate.
   - Build `UploadResult{DisplayName, ServerURL, PublicMeta: {"ca_data_b64": ...}, AuthType, AuthSecret: json.Marshal(secretFields)}`.
6. Handler generates a new `binding_id` (16 random bytes, URL-safe base64).
7. Handler calls `crypto.Encrypt(encKey, uploadResult.AuthSecret)` → `auth_blob`.
8. Handler `INSERT`s into `credential_bindings`. If this is the first binding for `(workspace_id, kind)`, `is_default = true`.
9. Handler returns `201 {id, display_name, server_url, auth_type, public_meta, is_default, created_at}`.

### Flow 2: Sandbox provisioning with merged kubeconfig

1. agentserver control plane decides to create a sandbox (user or agent request).
2. Existing code allocates sandbox row and generates `proxy_token`.
3. New branch: sandbox manager iterates `provider.Registry.All()`.
4. For the k8s provider, `db.ListBindings(wsid, "k8s")` returns metadata rows (no `auth_blob`).
5. `K8sProvider.BuildSandboxConfig(bindings, proxyToken, credproxyPublicURL)` generates a merged kubeconfig yaml:

   ```yaml
   apiVersion: v1
   kind: Config
   current-context: <default-binding-display-name>
   clusters:
     - name: <binding1-display-name>
       cluster:
         server: http://credentialproxy.agentserver.svc:8083/k8s/<binding1-id>
         insecure-skip-tls-verify: true
     - name: <binding2-display-name>
       cluster:
         server: http://credentialproxy.agentserver.svc:8083/k8s/<binding2-id>
         insecure-skip-tls-verify: true
   users:
     - name: <binding1-display-name>-user
       user:
         token: <sandbox-proxy-token>
     - name: <binding2-display-name>-user
       user:
         token: <sandbox-proxy-token>
   contexts:
     - name: <binding1-display-name>
       context:
         cluster: <binding1-display-name>
         user: <binding1-display-name>-user
     - name: <binding2-display-name>
       context:
         cluster: <binding2-display-name>
         user: <binding2-display-name>-user
   ```

   All contexts share the same `proxy_token`. Switching context via `kubectl config use-context` changes only which URL prefix is hit; the sandbox identity is invariant.

6. Returned as a slice containing one `SandboxConfigFile{SubPath: "kubeconfig", EnvVars: {"KUBECONFIG": "/var/run/agentserver/kubeconfig"}}` (the slice shape future-proofs for providers that emit multiple files).
7. Sandbox manager creates a k8s Secret `agent-sandbox-{short_id}-creds` with owner reference to the Sandbox CR. `data["kubeconfig"]` contains the merged yaml.
8. Pod spec is built with a volume + mount of the Secret at `/var/run/agentserver/`, and env var `KUBECONFIG=/var/run/agentserver/kubeconfig`.
9. Pod is created normally.

If the workspace has no k8s bindings, step 4's list is empty, `BuildSandboxConfig` is not called for this provider, and the sandbox gets no kubeconfig. `kubectl` inside the sandbox then fails with a connection error — this is the intended "no access" state.

### Flow 3: kubectl get pods (plain REST)

1. Agent inside sandbox runs `kubectl get pods -n default`.
2. kubectl reads `KUBECONFIG`, resolves `current-context`, looks up the cluster's `server` and the user's `token`.
3. kubectl sends `GET http://credentialproxy.agentserver.svc:8083/k8s/{bid}/api/v1/namespaces/default/pods` with `Authorization: Bearer <proxy_token>`.
4. credentialproxy middleware:
   - Extract `kind=k8s`, `binding_id=bid` via chi.
   - Extract `proxy_token` from Authorization header.
   - `auth.ValidateProxyToken(token)` → `{workspace_id, sandbox_id}` or 401.
   - `store.GetBinding(workspace_id, "k8s", bid)` or 404.
   - `crypto.Decrypt(encKey, row.auth_blob)` → `DecryptedBinding`.
   - Dispatch to `K8sProvider.ServeHTTP(w, r, decrypted)`.
5. `K8sProvider.ServeHTTP`:
   - Build `*http.Transport` using `public_meta.ca_data_b64` for the CA pool and `auth_secret` for credential injection (bearer or client cert).
   - `transport.DialContext` wraps `net.Dialer.DialContext` with an SSRF dial-time guard — layer 2 (rejects private IPs, handling DNS rebinding).
   - Strip the `/k8s/{bid}` prefix from `r.URL.Path`.
   - `r.Header.Del("Authorization")` — critical, otherwise the sandbox's proxy_token would leak to upstream and cause 401.
   - Build `upgradeAwareHandler := proxy.NewUpgradeAwareHandler(upstreamURL, transport, false, false, errorResponder)`.
   - `upgradeAwareHandler.ServeHTTP(w, r)`.
6. UpgradeAwareHandler forwards the (rewritten) request to upstream, streams the response back.
7. After completion, middleware emits `slog.Info("proxied", workspace_id=..., sandbox_id=..., kind="k8s", binding_id=bid, method="GET", path="/api/v1/namespaces/default/pods", status=200, latency_ms=42)`.

### Flow 4: kubectl exec (SPDY/WebSocket upgrade)

1. Agent runs `kubectl exec -it -n default mypod -- /bin/sh`.
2. kubectl sends `GET /k8s/{bid}/api/v1/namespaces/default/pods/mypod/exec?command=/bin/sh&stdin=true&stdout=true&stderr=true&tty=true` with `Upgrade: SPDY/3.1` (or WebSocket on kubectl ≥ 1.30) and `Authorization: Bearer <proxy_token>`.
3. credentialproxy middleware performs the same auth + decrypt + dispatch as Flow 3.
4. `K8sProvider.ServeHTTP` builds the transport and calls `UpgradeAwareHandler.ServeHTTP`.
5. `UpgradeAwareHandler` detects the upgrade header and:
   - Opens an HTTPS connection to the upstream, with its own upgrade request and injected credentials.
   - Waits for `101 Switching Protocols` from upstream.
   - Writes the 101 response back to the sandbox.
   - Hijacks both the sandbox and upstream TCP connections.
   - Copies bytes bidirectionally with `io.Copy` until either side closes.
6. Middleware emits `slog.Info("upgrade_open", workspace_id=..., sandbox_id=..., kind="k8s", binding_id=bid, path=...)` when the upgrade handshake completes, and `slog.Info("upgrade_closed", ..., duration_ms=..., bytes_in=..., bytes_out=...)` when either side closes. Two log lines per upgrade connection so long-running sessions are visible in tailed logs before they end.

`kubectl port-forward`, `kubectl attach`, and `kubectl cp` follow the same upgrade path. `kubectl proxy` is handled as normal REST (it's a client-side proxy that just makes repeated standard requests).

For upgrade connections, audit log granularity is connection-level — we do not parse SPDY frames or log individual commands. This matches the "minimal audit" decision.

### Error handling

| Situation | Response | Log level |
|---|---|---|
| Invalid or expired `proxy_token` | `401 Unauthorized` | info |
| `binding_id` not found in DB for this `(workspace, kind)` | `404 Not Found` `credential binding not found` | info |
| `binding_id` exists but belongs to a different workspace | `404 Not Found` (**not 403**, to avoid leaking existence) | warn |
| Upstream DNS resolution fails | `502 Bad Gateway` `upstream unreachable` | error (with `cluster_id`, redacted error) |
| Upstream connection refused | `502 Bad Gateway` `upstream unreachable` | error |
| Upstream TLS verification fails | `502 Bad Gateway` `upstream tls verification failed` | error |
| Upstream returns 401/403 | Pass through unchanged | info |
| Upstream returns 5xx | Pass through unchanged | info |
| AES-GCM decrypt failure (tag mismatch) | `500 Internal Server Error` `credential decryption failed` | error (alert-worthy: indicates DB tampering or key mismatch) |
| Startup: `CREDPROXY_ENCRYPTION_KEY` missing or wrong length | process exits 1 | fatal |
| Startup: cannot connect to DB | process exits 1 | fatal |
| DB query timeout (> 1s) on binding lookup | `503 Service Unavailable` | error |
| Binding deleted while a sandbox has an in-progress upgrade connection | New requests 404; existing upgrade connection is not proactively terminated in v1 | info |
| Upstream dial target is a private IP (SSRF layer 2) | `502 Bad Gateway` `upstream address rejected by policy` | warn |

Response bodies for errors are terse and never include sensitive context: no `server_url` from the binding, no raw upstream error strings, no certificate CNs.

## Security model

### Assets (descending sensitivity)

1. `CREDPROXY_ENCRYPTION_KEY` — single symmetric key; compromise yields plaintext of all `auth_blob` rows.
2. `credential_bindings.auth_blob` — ciphertext of all user-supplied k8s credentials.
3. Active sandbox `proxy_token`s — sandbox identity tokens; compromise allows impersonation within that sandbox's authorization scope.
4. Audit logs — paths may contain resource names (e.g., `/api/v1/namespaces/prod/secrets/db-password`).
5. credentialproxy process memory — holds decrypted credentials during request lifetime.

### Threat enumeration

| # | Threat | Defended | Residual / mitigation |
|---|---|---|---|
| 1 | Malicious agent in sandbox dumps its env / reads files trying to exfil real upstream credentials | Yes | Sandbox only sees `proxy_token` and credentialproxy URL. This is the core value of the design. |
| 2 | Agent uses its legitimate access to read upstream cluster secrets (`kubectl get secrets`) | Out of scope | This is upstream RBAC's job. Documentation must instruct users to bind narrowly-scoped ServiceAccounts, not cluster-admin. |
| 3 | Sandbox container escape to k8s node | Partially | Node has no upstream credentials either. Lateral movement to credentialproxy pod is equivalent to credentialproxy RCE (threat #5). |
| 4 | DB leak (backup, snapshot, SQL injection reads) | Yes | `auth_blob` is AES-GCM ciphertext. Useless without `CREDPROXY_ENCRYPTION_KEY`. |
| 5 | **credentialproxy process RCE** | Partially (the main accepted trade-off) | Attacker gains encKey (env) and DB read access → can decrypt all bindings. Mitigations: non-root container, read-only rootfs, dropped capabilities, scoped DB role, NetworkPolicy. |
| 6 | Stolen `proxy_token` used from outside the cluster | Yes | credentialproxy Service is cluster-internal only; NetworkPolicy rejects traffic from outside. |
| 7 | Uploaded kubeconfig points at internal network or cloud metadata IP (SSRF) | Yes (mandatory in v1) | See §SSRF protection below. |
| 8 | Uploaded kubeconfig points at attacker-controlled apiserver to attack client-go parser | Out of scope | Go's JSON/yaml parsers are sufficiently hardened for v1. |
| 9 | HTTP request smuggling / header injection | Yes | Go `net/http` is safe against smuggling; `binding_id` regex validation is defense-in-depth. |
| 10 | Cross-kind confusion (`/k8s/{id}` where `{id}` is actually a GitHub binding) | Yes | Store query filters by `(workspace_id, kind, binding_id)`. |
| 11 | Credentials leak into audit logs | Yes (by code discipline) | slog never receives `auth_blob`, decrypted values, request body, or response body. A lint/test asserts that known-secret strings never appear in captured log output. |
| 12 | Encryption key rotation required | Partially (documentation) | v1 does not support hot rotation. Operator runbook: stop writes → deploy new key as secondary → re-encrypt all rows in a migration → retire old key. |

### SSRF protection (v1 mandatory)

SSRF is promoted from a threat-model entry to a concrete v1 requirement because the exploit path is direct: a malicious workspace owner uploads a kubeconfig with `server: http://10.0.0.1/` or `http://169.254.169.254/latest/meta-data/` and uses their own sandbox to pull internal network / cloud metadata responses.

**Layer 1 — upload-time validation** (in `K8sProvider.ParseUpload`):

- Only `https://` scheme accepted.
- Parse the host. If it is a literal IP, reject if it falls in any of:
  - `10.0.0.0/8`
  - `172.16.0.0/12`
  - `192.168.0.0/16`
  - `127.0.0.0/8`
  - `169.254.0.0/16`
  - `0.0.0.0/8`
  - `::1/128`
  - `fc00::/7`
  - `fe80::/10`
- If the host is a hostname, do not resolve DNS at upload time (TOCTOU). Layer 2 handles this.

**Layer 2 — dial-time guard** (in `K8sProvider.buildUpstreamTransport`):

- Override `http.Transport.DialContext` with a wrapper that calls `net.DefaultResolver.LookupIPAddr`, then applies the same private-IP blocklist as layer 1 to every returned address before calling `net.Dial`.
- If any resolved IP is blocked, return `errors.New("dial refused: upstream address in private range")` without attempting the connection.
- This defeats DNS rebinding: even if upload-time DNS pointed at a public IP, dial-time DNS that returns a private IP is rejected.

**Override for internal testing**: `CREDPROXY_ALLOW_PRIVATE_UPSTREAMS=true` disables the guard. This is logged at `WARN` level on startup and included in the `/readyz` response so operators know it's active. Default is `false` in all standard deployments.

### Deployment hardening

**credentialproxy pod `securityContext`:**

```yaml
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
readOnlyRootFilesystem: true
allowPrivilegeEscalation: false
capabilities:
  drop: ["ALL"]
seccompProfile:
  type: RuntimeDefault
```

**NetworkPolicy** (conceptual):

- **Ingress**: from pods labeled `agentserver.io/sandbox: "true"` on port 8083. Nothing else.
- **Egress**:
  - To PostgreSQL pod on port 5432.
  - To agentserver pod on port 8080 (for `validate-proxy-token`).
  - To `0.0.0.0/0` on port 443 except RFC 1918 / metadata ranges — for external cluster API calls. (Belt-and-suspenders with the SSRF dial guard.)

**PostgreSQL role isolation:**

```sql
-- agentserver writes
GRANT SELECT, INSERT, UPDATE, DELETE ON credential_bindings TO agentserver;

-- credentialproxy reads only credential_bindings. It does not need
-- access to any other table: workspace_id is already a column on
-- credential_bindings, and sandbox proxy_token validation happens via
-- HTTP to agentserver rather than a direct DB query (intentional — keeps
-- credentialproxy's blast radius narrow and avoids duplicating the
-- token-validation logic that lives in agentserver).
CREATE ROLE credproxy_ro LOGIN PASSWORD '...';
GRANT SELECT ON credential_bindings TO credproxy_ro;
-- no other tables granted
```

**Strictness**: SSRF (both layers), chi `binding_id` regex, encrypted `auth_blob`, log discipline — all are **code-level requirements** that v1 must ship with. The securityContext, NetworkPolicy, and DB role isolation are **deployment-level recommendations** delivered as helm values or compose config, verifiable in integration environments but not enforced by unit tests.

## Testing strategy

### Layers

| Layer | Covers | Where it runs |
|---|---|---|
| Unit | Pure functions, parsers, crypto, merge logic | `go test ./...` — local + CI on every PR |
| Integration (in-process) | credentialproxy binary against stub upstream + stub agentserver | `go test -tags=integration` — CI on every PR |
| SPDY/WebSocket | Upgrade hijack + bidirectional copy | Tagged with integration — CI on every PR |
| End-to-end | Real kubectl against real kind cluster through real credentialproxy | `go test -tags=e2e` — nightly or manual trigger |

### Unit test coverage requirements

**`internal/crypto/aesgcm_test.go`**
- Encrypt → decrypt round-trip.
- Tamper any byte of ciphertext → `Decrypt` returns error.
- Wrong key → error.
- `LoadKeyFromEnv`: valid base64 / hex / raw; wrong length; missing env.

**`internal/credentialproxy/k8s/parse_test.go`** — table-driven with fixture yaml files under `testdata/`.
- Accept: single cluster/user/context bearer-token kubeconfig.
- Accept: client certificate + key kubeconfig.
- Accept: multi-cluster kubeconfig with valid `current-context`.
- Reject: `users[].user.exec` with message mentioning "exec plugin not supported".
- Reject: `users[].user.auth-provider`.
- Reject: `users[].user.username` + `password`.
- Reject: `clusters[].cluster.certificate-authority` (file path).
- Reject: `server: http://...`.
- Reject: `server: https://10.0.0.1/`, `https://169.254.169.254/`, and all other private-IP literals.
- Reject: invalid PEM CA.
- Reject: empty token, empty cert/key.

**`internal/credentialproxy/k8s/merge_test.go`**
- 0 bindings → empty `Config` (no error).
- 1 binding → `current-context` set to that one.
- N bindings, one `is_default=true` → `current-context` matches the default.
- N bindings, zero defaults → error (violated invariant).
- `proxy_token` appears in every user section.
- `credproxyURL` appears in every cluster `server` with shape `{credproxyURL}/k8s/{binding_id}`.
- Output yaml round-trips through `clientcmd.Load` without error.

**`internal/credentialproxy/k8s/transport_test.go`**
- Bearer mode: upstream observes injected `Authorization` header, not the sandbox's `proxy_token`.
- Client cert mode: upstream observes the expected TLS client certificate.
- CA pool mismatch → dial fails with tls error.
- SSRF dial guard: `10.0.0.1` rejected with the expected error.
- `CREDPROXY_ALLOW_PRIVATE_UPSTREAMS=true` allows `10.0.0.1`.

**`internal/credentialproxy/auth_test.go`**
- Stub agentserver returns 200 → returns parsed `{workspace_id, sandbox_id}`.
- Stub returns 401 → returns `(nil, nil)`.
- Stub returns 500 → returns error.
- Timeout → returns error.
- Empty token → returns error without hitting the network.

**`internal/credentialproxy/provider/registry_test.go`**
- Register + lookup successful round-trip.
- Duplicate kind registration panics or returns error.
- Lookup of unknown kind returns error.

### Integration tests (in-process, the primary coverage layer)

Use `httptest.NewServer` to stand up:
- Stub agentserver implementing only `/internal/validate-proxy-token`.
- Stub upstream k8s apiserver with self-signed TLS and canned responses.
- Real credentialproxy `Server` pointed at both.

Insert a binding row into a test Postgres (Docker-managed, same pattern used elsewhere in the repo) with `server_url` pointing at the stub upstream and `public_meta.ca_data_b64` matching the stub's self-signed cert.

**Test cases:**

- `GET /api/v1/namespaces/default/pods` returns 200 with the stub's body. Upstream request log shows: path prefix stripped, `Authorization` replaced with the injected token, `Host` header matches upstream.
- `GET /api/v1/pods?watch=1` streams chunked response, flusher works correctly, both sides close gracefully.
- `Authorization` header with unknown token → 401.
- `binding_id` belonging to a different workspace → 404.
- Upstream `connect refused` → 502.
- Upstream TLS mismatch (different CA in DB than stub actually uses) → 502.
- `binding_id` with path traversal characters → 400 (chi normalization + regex validation).

### SPDY/WebSocket tests

The stub upstream is extended to handle HTTP upgrade. Simplest implementation: `httptest.NewServer` with a hijack handler that, on a request with `Upgrade:`, sends `101 Switching Protocols` and then echoes bytes.

**Test cases:**

- `POST /api/v1/namespaces/default/pods/x/exec?...` with `Upgrade: SPDY/3.1` → 101 returned to client, bytes flow both directions, close propagates.
- Same with `Upgrade: websocket` — kubectl 1.30+ default.
- Upstream refuses upgrade (returns 400) → 400 is passed through; no connection hijack.
- Upstream returns 101 but then closes immediately → client sees clean EOF.
- Client closes mid-stream → credentialproxy closes upstream connection (prevents upstream zombie conns).

### End-to-end smoke (tag `e2e`)

Nightly or manual. Not a gating check for every PR.

```
kind create cluster --name credproxy-e2e
# Create a test ServiceAccount in kind, export its token into a test kubeconfig.
# Start agentserver + credentialproxy locally (docker-compose).
# Call the agentserver API to upload the kubeconfig as a binding.
# Use kubectl with --server=http://localhost:8083/k8s/{bid} --token=<proxy_token>:
#   kubectl get nodes
#   kubectl get pods -A
#   kubectl run test --image=nginx --restart=Never
#   kubectl wait --for=condition=Ready pod/test
#   kubectl exec test -- echo hello
#   kubectl logs test
#   kubectl port-forward test 8080:80 & sleep 2 && curl localhost:8080 && kill %1
#   kubectl delete pod test
# Clean up.
```

### Explicit non-tests in v1

- Connectivity against real AWS EKS / GCP GKE (no CI credentials, and exec-plugin kubeconfigs are rejected anyway).
- Load / soak testing (many concurrent `--watch` connections). Left for post-v1.
- Chaos testing (randomized upstream disconnects, DB failovers).
- Cross-provider tests (only one provider exists in v1).

## Extensibility notes

v2 adding a GitHub provider (sketch, to validate the v1 abstractions hold):

1. Create `internal/credentialproxy/github/github.go` implementing `provider.Provider`.
2. Register in `init()` so `provider.Registry.All()` includes it.
3. `GitHubProvider.ParseUpload` accepts a form with `{hostname, token}` for GitHub Enterprise, or just `{token}` defaulting to `github.com`.
4. `GitHubProvider.BuildSandboxConfig` returns two files:
   - `SubPath: "gh/hosts.yml"` with `EnvVars: {"GH_CONFIG_DIR": "/var/run/agentserver/gh"}` — for `gh` CLI.
   - `SubPath: ".netrc"` with `EnvVars: {"NETRC": "/var/run/agentserver/.netrc"}` — for `git clone https://github.com/...`.
5. `GitHubProvider.ServeHTTP` handles requests to `/github/{bid}/*` by injecting a real GitHub PAT into the `Authorization` header of the upstream request to `api.github.com/*`.
6. No DB schema changes required (`kind` column already supports it).
7. No routing changes required (`/{kind}/{id}/*` already dispatches).
8. No sandbox manager changes required (it already iterates `Registry.All()`).
9. The workspace CRUD handler on agentserver handles `POST /api/workspaces/{wsid}/credentials/github` by calling `Registry.Lookup("github").ParseUpload`.

The only file that requires changes outside `internal/credentialproxy/github/` is `cmd/serve.go` and `cmd/credentialproxy/main.go`, each adding one line to register the new provider.

## Open questions and future work

- **Short-lived token minting.** An alternative integration model for `git`/`gh` where the sandbox runs a tiny local credential helper that calls credentialproxy for a short-lived token instead of routing all HTTP traffic through us. Simpler for tools that don't honor `HTTP_PROXY` cleanly, but gives the sandbox a real (if short-lived) token.
- **Persistent audit log.** A `credential_audit_events` table with per-request metadata, retention cron, and a workspace-owner-facing query UI. Deferred because v1 chose stderr-only audit.
- **Encryption key rotation.** A multi-key scheme (current + previous key for decrypt, current for encrypt) with a migration workflow. Deferred; operator runbook covers v1.
- **HTTPS between sandbox and credentialproxy.** Requires a cluster-issued or self-signed cert trusted inside every sandbox. Deferred; intra-cluster HTTP is acceptable for v1.
- **Rate limiting per workspace.** No per-workspace quotas in v1. If abuse becomes a concern, can mirror llmproxy's RPD quota pattern.
- **Hot reload of merged kubeconfig.** If a workspace owner adds a new binding while a sandbox is running, v1 does not propagate the change. A future enhancement would watch `credential_bindings` and refresh the Secret; must handle the race with in-flight connections.
- **Exec plugin support.** Would let users paste EKS/GKE native kubeconfigs. Requires running user-specified commands inside credentialproxy's process or a sandbox — significant additional attack surface. Unlikely to be worth it; point users at service-account kubeconfigs instead.

## Appendix: dependencies

New Go module dependencies (all from the k8s ecosystem, already transitively used by the codebase via `client-go`):

- `k8s.io/apimachinery/pkg/util/proxy` — `UpgradeAwareHandler` for REST + SPDY/WebSocket transport.
- `k8s.io/client-go/tools/clientcmd` — kubeconfig parsing.
- `k8s.io/client-go/tools/clientcmd/api` — kubeconfig structure types.

No third-party dependencies are introduced outside of the k8s ecosystem.
