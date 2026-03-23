# ModelServer OAuth Integration Design

## Overview

Enable agentserver workspace users to connect their workspace to a modelserver project via standard OAuth 2.0 Authorization Code Flow, powered by Ory Hydra. This eliminates the need for users to manually copy-paste API keys — they simply log into modelserver, select a project, and the API key is automatically provisioned and configured.

## Context

- **agentserver** — Multi-tenant coding agent platform with BYOK (Bring Your Own Key) system. Workspaces store LLM config (base_url, api_key, models) in `workspace_llm_config` table.
- **modelserver** — LLM API gateway at `https://code.ai.cs.ac.cn`. Projects have members with roles, credit-based rate limiting, and API keys (`ms-` prefixed). Admin API on `:8081`, proxy on `:8080`.
- **Ory Hydra** — Open-source OAuth 2.0 / OIDC server deployed as a standalone Docker container alongside modelserver.

The two systems have independent user accounts. Users may need to log into modelserver separately during the OAuth flow.

## Architecture

```
                          modelserver domain (code.ai.cs.ac.cn)
┌────────────────────────────────────────────────────────────────────┐
│                                                                    │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────┐  │
│  │  Ory Hydra        │   │  modelserver     │   │ modelserver  │  │
│  │  Public :4444     │◄─►│  Admin API :8081 │   │ Proxy :8080  │  │
│  │  Admin  :4445     │   │  (Login/Consent  │   │              │  │
│  │                   │   │   Provider)      │   │              │  │
│  └──────────────────┘   └──────────────────┘   └──────────────┘  │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘

                          agentserver domain
┌────────────────────────────────────────────────────────────────────┐
│                                                                    │
│  ┌──────────────────┐   ┌──────────────────┐                      │
│  │  agentserver      │   │  Web Frontend    │                      │
│  │  API :8080        │   │  (React)         │                      │
│  │  (OAuth Client)   │   │                  │                      │
│  └──────────────────┘   └──────────────────┘                      │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

## OAuth 2.0 Flow

```
User Browser           agentserver          Hydra(:4444)        modelserver(:8081)
    │                      │                    │                     │
    │ 1. Click "Connect    │                    │                     │
    │    to ModelServer"   │                    │                     │
    │─────────────────────>│                    │                     │
    │                      │                    │                     │
    │ 2. 302 → Hydra /oauth2/auth              │                     │
    │    ?client_id=agentserver                 │                     │
    │    &redirect_uri=...                      │                     │
    │    &state=<random_hex>                    │                     │
    │    &scope=project:apikey                  │                     │
    │    &response_type=code                    │                     │
    │    &code_challenge=<S256 hash>            │                     │
    │    &code_challenge_method=S256            │                     │
    │<─────────────────────│                    │                     │
    │                      │                    │                     │
    │ 3. GET /oauth2/auth ─────────────────────>│                     │
    │                                           │                     │
    │ 4. 302 → modelserver /oauth/login         │                     │
    │    ?login_challenge=...                   │                     │
    │<──────────────────────────────────────────│                     │
    │                                                                 │
    │ 5. Login page (if not already logged in)                        │
    │<───────────────────────────────────────────────────────────────>│
    │ 6. Accept login challenge (Hydra Admin API)                     │
    │    302 → back to Hydra                                          │
    │<───────────────────────────────────────────────────────────────│
    │                                           │                     │
    │ 7. 302 → modelserver /oauth/consent       │                     │
    │    ?consent_challenge=...                 │                     │
    │<──────────────────────────────────────────│                     │
    │                                                                 │
    │ 8. Project selection page                                       │
    │    (lists user's projects with roles)                           │
    │<───────────────────────────────────────────────────────────────>│
    │                                                                 │
    │ 9. User selects project                                         │
    │───────────────────────────────────────────────────────────────>│
    │                                                                 │
    │ 10. modelserver:                                                │
    │     a) Creates API key for selected project                     │
    │     b) Accepts consent challenge with custom claims:            │
    │        {project_id, project_name, key_id}                       │
    │     302 → back to Hydra                                         │
    │<───────────────────────────────────────────────────────────────│
    │                                           │                     │
    │ 11. Hydra generates authorization code    │                     │
    │     302 → agentserver callback            │                     │
    │     ?code=...&state=...                   │                     │
    │<──────────────────────────────────────────│                     │
    │                      │                    │                     │
    │ 12. GET /api/auth/modelserver/callback    │                     │
    │     ?code=...&state=...                   │                     │
    │─────────────────────>│                    │                     │
    │                      │                    │                     │
    │                      │ 13. POST /oauth2/token                   │
    │                      │     {code, client_id, client_secret,     │
    │                      │      code_verifier}                      │
    │                      │───────────────────>│                     │
    │                      │ 14. {access_token} │                     │
    │                      │     (with custom   │                     │
    │                      │      claims)       │                     │
    │                      │<──────────────────│                     │
    │                      │                                          │
    │                      │ 15. GET /api/v1/oauth/key-info           │
    │                      │     Authorization: Bearer <access_token> │
    │                      │─────────────────────────────────────────>│
    │                      │ 16. {api_key, project_id, project_name}  │
    │                      │<────────────────────────────────────────│
    │                      │                                          │
    │                      │ 17. GET /v1/models                       │
    │                      │     x-api-key: <api_key>                │
    │                      │─────────────────────────────────────────>│
    │                      │ 18. {data: ["model-a", ...]}            │
    │                      │     (string[] → transform to {id,name}) │
    │                      │<────────────────────────────────────────│
    │                      │                                          │
    │                      │ 19. Save to workspace_llm_config:        │
    │                      │     base_url="https://code.ai.cs.ac.cn"  │
    │                      │     api_key=<api_key>                    │
    │                      │     models=<models>                      │
    │                      │     source="modelserver"                 │
    │                      │     source_meta={project_id,project_name}│
    │                      │                                          │
    │ 20. 302 → workspace  │                                          │
    │     settings page    │                                          │
    │<─────────────────────│                                          │
```

## modelserver Changes

### 1. Ory Hydra Deployment

Deploy Hydra as a standalone Docker container:

- **Public port (:4444)**: Exposed externally for `/oauth2/auth`, `/oauth2/token`, `/.well-known/openid-configuration`
- **Admin port (:4445)**: Internal only, used by modelserver to manage login/consent flows
- **Database**: Shared PostgreSQL (separate schema) or dedicated DB
- **Configuration**:
  - `URLS_LOGIN=https://code.ai.cs.ac.cn/oauth/login`
  - `URLS_CONSENT=https://code.ai.cs.ac.cn/oauth/consent`
  - `URLS_SELF_ISSUER=https://code.ai.cs.ac.cn`

Register agentserver as an OAuth client in Hydra:
```json
{
  "client_id": "agentserver",
  "client_secret": "<generated-secret>",
  "redirect_uris": ["https://<agentserver-domain>/api/auth/modelserver/callback"],
  "grant_types": ["authorization_code"],
  "response_types": ["code"],
  "scope": "project:apikey",
  "token_endpoint_auth_method": "client_secret_post"
}
```

### 2. Login Provider

**Session mechanism:** modelserver currently uses JWT Bearer tokens (short-lived, 15-minute access tokens) without cookie-based sessions. For the Hydra login flow, modelserver needs a new cookie-based session mechanism:
- On successful login during the OAuth flow, set a `modelserver-oauth-session` cookie containing an encrypted `{user_id, expires_at}` payload (AES-GCM with the server's encryption key, 24-hour TTL)
- This cookie is HttpOnly, Secure, SameSite=Lax, scoped to the modelserver domain
- On subsequent OAuth flows, the login provider checks this cookie to skip the login screen

**Endpoints:**

`GET /oauth/login?login_challenge=<challenge>`
- Calls Hydra Admin API `GET /admin/oauth2/auth/requests/login?login_challenge=<challenge>` to get challenge details
- If `modelserver-oauth-session` cookie is present and valid → accept the login challenge immediately (Hydra's `skip` mechanism)
- Otherwise → render login page

`POST /oauth/login`
- Request body: `{login_challenge, email, password}` (or OAuth provider redirect)
- Validates user credentials (reuse existing auth logic from `internal/auth/`)
- On success:
  - Sets `modelserver-oauth-session` cookie
  - Calls Hydra Admin API `PUT /admin/oauth2/auth/requests/login/accept` with `{subject: <user_id>}`
  - Redirects to the URL returned by Hydra

**Implementation notes:**
- The login page can offer the same login options as the main modelserver UI (GitHub, Google, OIDC, etc.)
- For OAuth provider login (GitHub/Google), the flow is: login page → redirect to GitHub → callback → set session cookie → accept Hydra login challenge → redirect back to Hydra. This creates a nested OAuth flow which works but adds complexity. For v1, email/password login is sufficient.

### 3. Consent Provider

**Endpoints:**

`GET /oauth/consent?consent_challenge=<challenge>`
- Calls Hydra Admin API `GET /admin/oauth2/auth/requests/consent?consent_challenge=<challenge>` to get challenge details and user identity
- Fetches user's projects (where user is owner, maintainer, or developer)
- Renders project selection page showing project name, description, role, and subscription plan

`POST /oauth/consent`
- Request body: `{consent_challenge, project_id}`
- Validates user has access to the selected project
- Creates an API key for the project via `store.CreateAPIKey()`:
  1. `store.CreateAPIKey(projectID, userID, name, description, allowedModels, expiresAt)` returns `(APIKey, plaintextKey, error)`
  2. The returned `APIKey.ID` is a `UUID` (generated by PostgreSQL `gen_random_uuid()` via `RETURNING id`)
  3. Key params:
     - Name: `"agentserver-oauth-<timestamp>"`
     - Description: `"Auto-created via agentserver OAuth integration"`
     - Allowed models: `nil` (no restrictions, allows all models)
     - ExpiresAt: `nil` (no expiration)
  4. Stores the plaintext API key in `oauth_pending_keys` table, encrypted with AES (using `key_id` as the primary key referencing `api_keys.id UUID`)
- Calls Hydra Admin API `PUT /admin/oauth2/auth/requests/consent/accept` with:
  - `grant_scope: ["project:apikey"]`
  - `session.access_token: {project_id, project_name, key_id}`
  - `remember: false` (always show project selection — the consent screen IS the project selector)
- Redirects to the URL returned by Hydra

### 4. Key Info Endpoint

`GET /api/v1/oauth/key-info`
- **Auth**: Hydra-issued access_token (validated via Hydra's token introspection or JWT verification)
- Extracts `key_id` from token claims
- Returns the API key plaintext + project metadata
- The API key plaintext is stored temporarily (e.g., in-memory cache or encrypted DB column with short TTL) between consent acceptance and this call

**Response:**
```json
{
  "api_key": "ms-...",
  "project_id": "uuid",
  "project_name": "My Project"
}
```

**Security:** This endpoint is the only place the API key plaintext is returned. It should:
- Only work once per key_id (mark as retrieved after first call)
- Expire the temporary storage after 5 minutes
- Require a valid Hydra access_token with matching key_id claim

### 5. New Database Migration

```sql
-- Temporary storage for OAuth-generated API key plaintexts
CREATE TABLE oauth_pending_keys (
    key_id UUID PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    encrypted_key BYTEA NOT NULL,          -- AES-encrypted API key plaintext
    expires_at TIMESTAMPTZ NOT NULL,       -- 5 minutes from creation
    retrieved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_oauth_pending_keys_expires ON oauth_pending_keys(expires_at);
```

A background job or lazy cleanup deletes expired rows.

### 6. Login/Consent Frontend Pages

modelserver needs to serve HTML pages for the login and consent flows. These can be:
- Server-side rendered Go templates (simplest, no additional build pipeline)
- Or a small SPA served from the admin API

**Login page**: Email/password form + OAuth provider buttons (matching existing modelserver login options).

**Consent page**: List of user's projects as selectable cards showing:
- Project name and description
- User's role in the project
- Current subscription plan
- A "Select" button

## agentserver Changes

### 1. New Configuration

Environment variables:
```
MODELSERVER_OAUTH_CLIENT_ID       - OAuth client ID registered in Hydra (e.g., "agentserver")
MODELSERVER_OAUTH_CLIENT_SECRET   - OAuth client secret
MODELSERVER_OAUTH_AUTH_URL        - Hydra authorization endpoint (https://code.ai.cs.ac.cn/oauth2/auth)
MODELSERVER_OAUTH_TOKEN_URL       - Hydra token endpoint (https://code.ai.cs.ac.cn/oauth2/token)
MODELSERVER_OAUTH_REDIRECT_URI    - Callback URL (https://<agentserver>/api/auth/modelserver/callback)
MODELSERVER_KEY_INFO_URL          - Key info endpoint (https://code.ai.cs.ac.cn/api/v1/oauth/key-info)
MODELSERVER_BASE_URL              - Proxy base URL (https://code.ai.cs.ac.cn)
```

Add to `internal/config` or directly in server initialization.

### 2. New Backend Endpoints

**`GET /api/workspaces/{id}/modelserver/connect`**
- Requires: authenticated user with owner/maintainer role
- State parameter handling (follows the same pattern as existing OIDC in `internal/auth/oidc.go`):
  1. Generate a random 32-byte hex string as `state`
  2. Store in cookie `modelserver-oauth-state` (HttpOnly, Secure, SameSite=Lax, 10-minute expiry)
  3. Store workspace_id in a separate cookie `modelserver-oauth-wsid` (same attributes)
- PKCE (RFC 7636):
  1. Generate `code_verifier`: 32 random bytes, base64url-encoded
  2. Compute `code_challenge`: SHA256(code_verifier), base64url-encoded
  3. Store `code_verifier` in cookie `modelserver-oauth-pkce` (same attributes)
- Redirects to: `MODELSERVER_OAUTH_AUTH_URL?client_id=...&redirect_uri=...&state=...&scope=project:apikey&response_type=code&code_challenge=...&code_challenge_method=S256`

**`GET /api/auth/modelserver/callback`**
- Query params: `code`, `state`
- Processing:
  1. Validate `state` matches `modelserver-oauth-state` cookie (same random-hex comparison as existing OIDC)
  2. Extract `workspace_id` from `modelserver-oauth-wsid` cookie
  3. Extract `code_verifier` from `modelserver-oauth-pkce` cookie
  4. Clear all three OAuth cookies
  5. Verify user has owner/maintainer role on the workspace
  6. Exchange code for access_token: `POST MODELSERVER_OAUTH_TOKEN_URL` with `{grant_type: "authorization_code", code, client_id, client_secret, redirect_uri, code_verifier}`
     - Timeout: 10 seconds
  7. Fetch key info: `GET MODELSERVER_KEY_INFO_URL` with `Authorization: Bearer <access_token>`
     - Response: `{api_key, project_id, project_name}`
     - Timeout: 10 seconds
  8. Fetch models: `GET MODELSERVER_BASE_URL/v1/models` with `x-api-key: <api_key>` header (matching modelserver's primary API key auth convention)
     - Response: `{data: ["model-a", "model-b", ...]}`
     - **Transform string[] to []LLMModel**: for each model string `s`, create `LLMModel{ID: s, Name: s}` (use the model ID string as both `id` and `name`, since modelserver returns only IDs)
     - Timeout: 10 seconds; on failure, use empty models list (non-fatal)
  9. If workspace already has a modelserver-sourced config with a `key_id` in `source_meta`, attempt to revoke the old key:
     - `DELETE MODELSERVER_BASE_URL/api/v1/projects/{old_project_id}/keys/{old_key_id}` with `x-api-key: <old_api_key>` — best effort, log errors but do not block
  10. Save to workspace_llm_config: `SetWorkspaceLLMConfig(workspaceID, baseURL, apiKey, models, "modelserver", sourceMeta)` where `sourceMeta = {project_id, project_name, key_id}`
  11. Redirect to frontend: `/workspaces/{id}?tab=settings&modelserver=connected`
- Error handling: On any failure (steps 1-7, 10), redirect to `/workspaces/{id}?tab=settings&modelserver=error&message=<urlencoded>`

### 3. Database Migration

```sql
-- 006_llm_config_source.sql
ALTER TABLE workspace_llm_config
    ADD COLUMN source TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN source_meta JSONB;
```

- `source`: `"manual"` (hand-entered BYOK) or `"modelserver"` (OAuth flow)
- `source_meta`: For modelserver source: `{"project_id": "...", "project_name": "...", "key_id": "..."}`

### Updated Go function signatures

```go
// internal/db/llm_config.go

type WorkspaceLLMConfig struct {
    WorkspaceID string     `json:"workspace_id"`
    BaseURL     string     `json:"base_url"`
    APIKey      string     `json:"api_key"`
    Models      []LLMModel `json:"models"`
    Source      string     `json:"source"`         // "manual" or "modelserver"
    SourceMeta  *json.RawMessage `json:"source_meta,omitempty"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
}

// Updated to include source fields
func (db *DB) SetWorkspaceLLMConfig(
    workspaceID, baseURL, apiKey string,
    models []LLMModel,
    source string,
    sourceMeta json.RawMessage,
) error

// GetWorkspaceLLMConfig returns the full config including source fields
func (db *DB) GetWorkspaceLLMConfig(workspaceID string) (*WorkspaceLLMConfig, error)
```

The existing manual BYOK handler (`handleSetWorkspaceLLMConfig`) calls `SetWorkspaceLLMConfig` with `source="manual"` and `sourceMeta=nil`.

### 4. API Response Changes

`GET /api/workspaces/{id}/llm-config` response adds:
```json
{
  "configured": true,
  "base_url": "https://code.ai.cs.ac.cn",
  "api_key": "ms-...XXXX",
  "models": [...],
  "source": "modelserver",
  "source_meta": {
    "project_id": "...",
    "project_name": "My Project"
  },
  "updated_at": "..."
}
```

### 5. Frontend Changes (WorkspaceDetail.tsx SettingsTab)

**New "Connect to ModelServer" section:**

When `!configured` or `source === "manual"`:
- Show existing manual BYOK configuration UI
- Add a divider: "Or connect to ModelServer"
- "Connect to ModelServer" button → navigates to `GET /api/workspaces/{id}/modelserver/connect`

When `source === "modelserver"`:
- Show: "Connected to ModelServer project: **{project_name}**"
- Show: masked API key, model list (same as current BYOK display)
- "Reconnect" button → re-initiates OAuth flow
- "Disconnect" button → calls DELETE /api/workspaces/{id}/llm-config

**URL parameter handling:**
- On mount, check for `?modelserver=connected` → show success toast
- Check for `?modelserver=error&message=...` → show error toast

### 6. API Client (api.ts)

No new API client methods needed for the OAuth flow (it's all server-side redirects via `window.location.href`, not fetch calls — the browser leaves the SPA and returns via the final redirect at step 20, which the SPA router handles).

The existing `getWorkspaceLLMConfig` and `deleteWorkspaceLLMConfig` remain unchanged. The response type adds:

```typescript
interface WorkspaceLLMConfig {
  configured: boolean
  base_url?: string
  api_key?: string
  models?: LLMModel[]
  source?: string          // "manual" | "modelserver"
  source_meta?: {
    project_id?: string
    project_name?: string
    key_id?: string
  }
  updated_at?: string
}
```

## Error Handling

| Scenario | Behavior |
|----------|----------|
| User cancels OAuth flow | Hydra redirects with `error=access_denied` → agentserver shows "Authorization cancelled" |
| Code exchange fails | Redirect to settings with error message |
| Key info retrieval fails | Redirect to settings with error message |
| Models fetch fails | Save config with empty models list, show warning |
| API key revoked later | Sandbox LLM calls fail at runtime; user sees errors and can reconnect |
| Project suspended | Same as above — runtime failure, user reconnects with different project |
| Re-authorization | New API key replaces old one in workspace_llm_config; agentserver attempts best-effort revocation of old key via modelserver API (using old key_id from source_meta). If revocation fails, old key remains (user can clean up manually via modelserver UI). |
| Concurrent OAuth flows | Last one wins (upsert semantics in SetWorkspaceLLMConfig) |

## Security Considerations

1. **API key never in browser**: The API key plaintext only travels server-to-server (steps 15-16 in the flow). The authorization code in the URL is a one-time-use short-lived token.
2. **State parameter**: Random hex value stored in cookie, compared on callback — follows the same pattern as existing OIDC implementation in agentserver. Workspace ID stored in a separate cookie (not in the state URL parameter).
3. **PKCE (RFC 7636)**: Used even though agentserver is a confidential client, as defense-in-depth against authorization code interception. S256 challenge method.
4. **Client secret**: Stored only in agentserver backend config, never exposed to frontend.
5. **Key info endpoint**: One-time retrieval with 5-minute TTL prevents replay.
6. **Scope limitation**: OAuth scope `project:apikey` limits what the authorization grants.
7. **Role checks**: Only workspace owner/maintainer can initiate the connect flow.
8. **Consent not remembered**: Hydra consent is set to `remember: false` so users always see the project selection screen, preventing unintended project binding.
9. **API key at rest**: Stored in plaintext in `workspace_llm_config` (consistent with existing manual BYOK behavior). Future improvement: encrypt at rest.

## Testing Plan

1. **Unit tests**: State encryption/decryption, callback parameter validation, config storage
2. **Integration tests**: Full OAuth flow with mock Hydra responses
3. **E2E tests**: Complete browser flow (agentserver → Hydra → modelserver login → project select → callback → config saved)
4. **Error cases**: Invalid state, expired code, revoked key, network failures
