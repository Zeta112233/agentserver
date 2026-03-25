# NanoClaw Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add NanoClaw as a third sandbox type in agentserver with K8s Pod management, llmproxy integration, and WeChat message bridging.

**Architecture:** NanoClaw runs as a K8s Pod (like openclaw), with agents running directly via Claude Agent SDK (no Docker inside Pod). WeChat messages are bridged through agentserver's iLink backend. Config is injected via `NANOCLAW_CONFIG_CONTENT` environment variable.

**Tech Stack:** Go (backend), TypeScript/React (frontend), PostgreSQL (DB), K8s (orchestration), Node.js (NanoClaw container)

**Spec:** `docs/superpowers/specs/2026-03-24-nanoclaw-integration-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/sandbox/config.go` | Add NanoClaw config fields + `BuildNanoclawConfig()` |
| `internal/sandbox/config_test.go` | Tests for `BuildNanoclawConfig()` |
| `internal/process/process.go` | Add `NanoclawBridgeSecret` to `StartOptions` |
| `internal/sbxstore/store.go` | Add `NanoclawBridgeSecret` to `Sandbox` struct |
| `internal/db/sandboxes.go` | Add `NanoclawBridgeSecret` to Sandbox struct + `UpdateSandboxNanoclawBridgeSecret()` |
| `internal/db/migrations/008_nanoclaw_bridge_secret.sql` | Add column to sandboxes table |
| `internal/sandbox/manager.go` | Add nanoclaw Pod spec, runtime class, health probe |
| `internal/server/server.go` | Type validation, creation logic, guard updates, bridge endpoints |
| `Dockerfile.nanoclaw` | Container image with weixin channel + process-runner |
| `nanoclaw-entrypoint.sh` | Config injection entrypoint script |
| `nanoclaw-weixin-channel/index.ts` | WeChat channel for NanoClaw (bridge mode) |
| `nanoclaw-patches/process-runner.ts` | No-container mode agent execution adapter |
| `web/src/components/CreateSandboxModal.tsx` | Add NanoClaw type option |
| `web/src/components/SandboxDetail.tsx` | Update openclaw-specific guards |
| `internal/db/migrations/009_nanoclaw_weixin_bridge.sql` | Bridge credentials + reverse lookup (Phase 3) |
| `internal/db/weixin_bindings.go` | `GetSandboxByBotID()`, `SaveBotCredentials()` (Phase 3) |
| `internal/weixin/ilink.go` | `SendMessage()`, `RegisterWebhook()` (Phase 3, blocked) |

---

## Phase 1: Sandbox Type + Pod Management

### Task 1: Add NanoClaw Config Fields and BuildNanoclawConfig

**Files:**
- Modify: `internal/sandbox/config.go`
- Create: `internal/sandbox/config_test.go`

- [ ] **Step 1: Write test for BuildNanoclawConfig**

Create `internal/sandbox/config_test.go`:

```go
package sandbox

import (
	"strings"
	"testing"
)

func TestBuildNanoclawConfig_Basic(t *testing.T) {
	result := BuildNanoclawConfig("https://proxy.example.com", "tok-123", "Andy", "", "", "", "")

	if !strings.Contains(result, "ANTHROPIC_BASE_URL=https://proxy.example.com") {
		t.Errorf("missing ANTHROPIC_BASE_URL, got: %s", result)
	}
	if !strings.Contains(result, "ANTHROPIC_API_KEY=tok-123") {
		t.Errorf("missing ANTHROPIC_API_KEY, got: %s", result)
	}
	if !strings.Contains(result, "ASSISTANT_NAME=Andy") {
		t.Errorf("missing ASSISTANT_NAME, got: %s", result)
	}
	if !strings.Contains(result, "NANOCLAW_NO_CONTAINER=true") {
		t.Errorf("missing NANOCLAW_NO_CONTAINER, got: %s", result)
	}
	// Should NOT contain weixin vars when not enabled
	if strings.Contains(result, "NANOCLAW_WEIXIN_BRIDGE_URL") {
		t.Errorf("should not contain NANOCLAW_WEIXIN_BRIDGE_URL when weixin disabled")
	}
}

func TestBuildNanoclawConfig_WithWeixin(t *testing.T) {
	result := BuildNanoclawConfig("https://proxy.example.com", "tok-123", "Andy",
		"https://bridge.example.com/weixin", "secret-abc", "", "")

	if !strings.Contains(result, "NANOCLAW_WEIXIN_BRIDGE_URL=https://bridge.example.com/weixin") {
		t.Errorf("missing NANOCLAW_WEIXIN_BRIDGE_URL, got: %s", result)
	}
	if !strings.Contains(result, "NANOCLAW_BRIDGE_SECRET=secret-abc") {
		t.Errorf("missing NANOCLAW_BRIDGE_SECRET, got: %s", result)
	}
}

func TestBuildNanoclawConfig_BYOK(t *testing.T) {
	result := BuildNanoclawConfig("https://proxy.example.com", "tok-123", "Andy",
		"", "", "https://custom.llm.com", "custom-key-456")

	if !strings.Contains(result, "ANTHROPIC_BASE_URL=https://custom.llm.com") {
		t.Errorf("BYOK should override base URL, got: %s", result)
	}
	if !strings.Contains(result, "ANTHROPIC_API_KEY=custom-key-456") {
		t.Errorf("BYOK should override API key, got: %s", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/agentserver && go test ./internal/sandbox/ -run TestBuildNanoclawConfig -v`
Expected: FAIL — `BuildNanoclawConfig` undefined

- [ ] **Step 3: Add NanoClaw config fields and BuildNanoclawConfig**

In `internal/sandbox/config.go`, add three fields to `Config` struct after `OpenclawWeixinEnabled`:

```go
NanoclawImage            string
NanoclawRuntimeClassName string
NanoclawWeixinEnabled    bool
```

Add to `DefaultConfig()`:

```go
NanoclawImage:            os.Getenv("NANOCLAW_IMAGE"),
NanoclawRuntimeClassName: os.Getenv("NANOCLAW_RUNTIME_CLASS"),
NanoclawWeixinEnabled:    os.Getenv("NANOCLAW_WEIXIN_ENABLED") == "true",
```

Add `BuildNanoclawConfig` function after `BuildOpenclawConfig`:

```go
// BuildNanoclawConfig returns the .env file content for a NanoClaw sandbox.
// The result is injected via NANOCLAW_CONFIG_CONTENT env var and written
// to /app/.env by the container entrypoint.
func BuildNanoclawConfig(proxyBaseURL, proxyToken, assistantName string, weixinBridgeURL, bridgeSecret string, byokBaseURL, byokAPIKey string) string {
	baseURL := proxyBaseURL
	apiKey := proxyToken
	if byokBaseURL != "" {
		baseURL = byokBaseURL
		apiKey = byokAPIKey
	}

	var lines []string
	lines = append(lines, "ANTHROPIC_BASE_URL="+baseURL)
	lines = append(lines, "ANTHROPIC_API_KEY="+apiKey)
	if assistantName == "" {
		assistantName = "Andy"
	}
	lines = append(lines, "ASSISTANT_NAME="+assistantName)
	lines = append(lines, "NANOCLAW_NO_CONTAINER=true")

	if weixinBridgeURL != "" {
		lines = append(lines, "NANOCLAW_WEIXIN_BRIDGE_URL="+weixinBridgeURL)
	}
	if bridgeSecret != "" {
		lines = append(lines, "NANOCLAW_BRIDGE_SECRET="+bridgeSecret)
	}

	return strings.Join(lines, "\n") + "\n"
}
```

Add `"strings"` to imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /root/agentserver && go test ./internal/sandbox/ -run TestBuildNanoclawConfig -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/config.go internal/sandbox/config_test.go
git commit -m "feat(sandbox): add NanoClaw config fields and BuildNanoclawConfig"
```

---

### Task 2: Add NanoclawBridgeSecret to Process StartOptions

**Files:**
- Modify: `internal/process/process.go`

- [ ] **Step 1: Add NanoclawBridgeSecret field to StartOptions**

In `internal/process/process.go`, add after `CustomModels` field (line 36):

```go
NanoclawBridgeSecret string        // nanoclaw only: shared secret for bridge HTTP auth
```

Update the comment on `SandboxType` field (line 29):

```go
SandboxType      string        // "opencode", "openclaw", or "nanoclaw"
```

- [ ] **Step 2: Commit**

```bash
git add internal/process/process.go
git commit -m "feat(process): add NanoclawBridgeSecret to StartOptions"
```

---

### Task 3: DB Migration for nanoclaw_bridge_secret Column

**Files:**
- Create: `internal/db/migrations/008_nanoclaw_bridge_secret.sql`

Note: Existing migrations go up to `007_drop_username.sql`. This must be `008`.

- [ ] **Step 1: Create migration file**

```sql
-- Add nanoclaw bridge secret column to sandboxes table.
-- Stores the shared secret for HTTP auth between agentserver and NanoClaw pod.
ALTER TABLE sandboxes ADD COLUMN nanoclaw_bridge_secret TEXT;
```

- [ ] **Step 2: Verify numbering**

Run: `ls internal/db/migrations/` — confirm `008_nanoclaw_bridge_secret.sql` is the next sequential file.

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/008_nanoclaw_bridge_secret.sql
git commit -m "feat(db): add nanoclaw_bridge_secret column to sandboxes"
```

---

### Task 4: Update DB Layer and Sandbox Store for NanoclawBridgeSecret

**Files:**
- Modify: `internal/db/sandboxes.go` — add field to `Sandbox` struct, `sandboxColumns`, `scanSandbox`, and new `UpdateSandboxNanoclawBridgeSecret` function
- Modify: `internal/sbxstore/store.go` — add field to sbxstore `Sandbox` struct and mapping

The DB layer uses `database/sql` (standard library), NOT pgx. All queries use `db.Exec(query, args...)`, `db.QueryRow(query, args...)`, `db.Query(query, args...)`.

- [ ] **Step 1: Add NanoclawBridgeSecret to db.Sandbox struct**

In `internal/db/sandboxes.go`, add after `TunnelToken` (line 22):

```go
NanoclawBridgeSecret sql.NullString
```

- [ ] **Step 2: Update sandboxColumns**

At line 45, append `, nanoclaw_bridge_secret` to the `sandboxColumns` const:

```go
const sandboxColumns = `id, workspace_id, name, type, status, is_local, short_id, sandbox_name, pod_ip, proxy_token, opencode_token, openclaw_token, tunnel_token, last_activity_at, created_at, paused_at, last_heartbeat_at, cpu, memory, idle_timeout, nanoclaw_bridge_secret`
```

- [ ] **Step 3: Update scanSandbox**

At line 49, add `&s.NanoclawBridgeSecret` to the Scan call (at the end, after `&s.IdleTimeout`):

```go
err := scanner.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Type, &s.Status, &s.IsLocal, &s.ShortID, &s.SandboxName, &s.PodIP, &s.ProxyToken, &s.OpencodeToken, &s.OpenclawToken, &s.TunnelToken, &s.LastActivityAt, &s.CreatedAt, &s.PausedAt, &s.LastHeartbeatAt, &s.CPU, &s.Memory, &s.IdleTimeout, &s.NanoclawBridgeSecret)
```

- [ ] **Step 4: Add UpdateSandboxNanoclawBridgeSecret function**

```go
// UpdateSandboxNanoclawBridgeSecret stores the bridge secret for a nanoclaw sandbox.
func (db *DB) UpdateSandboxNanoclawBridgeSecret(id, secret string) error {
	_, err := db.Exec(
		`UPDATE sandboxes SET nanoclaw_bridge_secret = $1 WHERE id = $2`,
		secret, id,
	)
	if err != nil {
		return fmt.Errorf("update nanoclaw bridge secret: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Add NanoclawBridgeSecret to sbxstore.Sandbox struct**

In `internal/sbxstore/store.go`, add to `Sandbox` struct after `OpenclawToken` (line 22):

```go
NanoclawBridgeSecret string     `json:"-"`
```

- [ ] **Step 6: Update dbSandboxToSandbox mapping**

Find the `dbSandboxToSandbox` function in `internal/sbxstore/store.go` and add:

```go
NanoclawBridgeSecret: dbSbx.NanoclawBridgeSecret.String,
```

(Uses `.String` because the DB field is `sql.NullString`.)

- [ ] **Step 7: Commit**

```bash
git add internal/db/sandboxes.go internal/sbxstore/store.go
git commit -m "feat(db): add NanoclawBridgeSecret to sandbox model and DB layer"
```

---

### Task 5: Update Server — Type Validation and Creation Logic

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: Update type validation in handleCreateSandbox**

At line 1130, change:

```go
if sandboxType != "opencode" && sandboxType != "openclaw" {
    http.Error(w, "invalid sandbox type: must be opencode or openclaw", http.StatusBadRequest)
```

To:

```go
if sandboxType != "opencode" && sandboxType != "openclaw" && sandboxType != "nanoclaw" {
    http.Error(w, "invalid sandbox type: must be opencode, openclaw, or nanoclaw", http.StatusBadRequest)
```

- [ ] **Step 2: Add nanoclaw credential generation**

At lines 1209-1214, add a nanoclaw case to the switch:

```go
switch sandboxType {
case "openclaw":
    openclawToken = generatePassword()
case "nanoclaw":
    // NanoClaw uses a bridge secret instead of openclaw/opencode tokens.
    // The bridge secret is stored separately after sandbox creation.
default: // "opencode"
    opencodeToken = generatePassword()
}
```

After the `Store.Create()` call (line 1221-1231), add:

```go
// Generate and store bridge secret for nanoclaw sandboxes.
if sandboxType == "nanoclaw" {
    bridgeSecret := generatePassword()
    if err := s.DB.UpdateSandboxNanoclawBridgeSecret(id, bridgeSecret); err != nil {
        log.Printf("failed to store nanoclaw bridge secret: %v", err)
    }
    sbx.NanoclawBridgeSecret = bridgeSecret
}
```

(This approach uses a separate update to avoid changing the long `Create()` parameter list.)

- [ ] **Step 3: Add nanoclaw to startOpts**

After the startOpts construction (lines 1234-1243), add:

```go
if sandboxType == "nanoclaw" {
    startOpts.NanoclawBridgeSecret = sbx.NanoclawBridgeSecret
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): add nanoclaw type validation and creation logic"
```

---

### Task 6: Update K8s Manager for NanoClaw Pod Spec

**Files:**
- Modify: `internal/sandbox/manager.go`

- [ ] **Step 1: Add nanoclaw case to runtimeClassNameFor**

At `runtimeClassNameFor()` (line 730), add after the openclaw case:

```go
case "nanoclaw":
    if m.cfg.NanoclawRuntimeClassName != "" {
        return strPtr(m.cfg.NanoclawRuntimeClassName)
    }
```

- [ ] **Step 2: Add nanoclaw case to StartContainerWithIP**

In `StartContainerWithIP()`, at the `switch opts.SandboxType` block (line 340), add a new case before `default`:

```go
case "nanoclaw":
    if m.cfg.NanoclawImage != "" {
        sandboxImage = m.cfg.NanoclawImage
    }
    containerPort = 3002 // Health/bridge endpoint
    // Build NanoClaw config as .env content.
    // BYOK overrides are handled inside BuildNanoclawConfig:
    // - proxyBaseURL/proxyToken are the default (llmproxy)
    // - byokBaseURL/byokAPIKey override when non-empty
    weixinBridgeURL := ""
    bridgeSecret := ""
    if m.cfg.NanoclawWeixinEnabled && opts.NanoclawBridgeSecret != "" {
        // TODO: construct actual bridge URL from agentserver base domain
        bridgeSecret = opts.NanoclawBridgeSecret
    }
    nanoclawCfg := BuildNanoclawConfig(
        proxyBaseURL, opts.ProxyToken, "Andy",
        weixinBridgeURL, bridgeSecret,
        opts.BYOKBaseURL, opts.BYOKAPIKey,
    )
    containerEnv = append(containerEnv, corev1.EnvVar{Name: "NANOCLAW_CONFIG_CONTENT", Value: nanoclawCfg})
```

Note: `BuildNanoclawConfig` handles BYOK internally — when `byokBaseURL` is non-empty, it overrides the proxy values. This ensures BYOK works correctly for nanoclaw sandboxes.

- [ ] **Step 3: Add nanoclaw workingDir**

At the `switch opts.SandboxType` block for `workingDir` (line 451), add:

```go
case "nanoclaw":
    workingDir = "/app"
```

- [ ] **Step 4: Update readiness probe for nanoclaw**

After the mainContainer creation (around line 467), add an override for nanoclaw to use HTTP GET instead of TCP:

```go
if opts.SandboxType == "nanoclaw" {
    mainContainer.ReadinessProbe = &corev1.Probe{
        ProbeHandler: corev1.ProbeHandler{
            HTTPGet: &corev1.HTTPGetAction{
                Path: "/health",
                Port: intstr.FromInt32(int32(containerPort)),
            },
        },
        InitialDelaySeconds: 5,
        PeriodSeconds:       5,
        FailureThreshold:    30,
    }
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/manager.go
git commit -m "feat(manager): add nanoclaw Pod spec with health probe"
```

---

### Task 7: Update Response Builders and Guard Conditions

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: Update toSandboxResponse to handle nanoclaw type**

At `toSandboxResponse()` (line 467, the switch is at line 487), change the switch to explicitly handle nanoclaw:

```go
switch sbx.Type {
case "openclaw":
    resp.OpenclawURL = "https://" + s.OpenclawSubdomainPrefix + "-" + subID + "." + domain + "/auth?token=" + authToken
case "nanoclaw":
    // NanoClaw has no Web UI — no URL to generate
default: // "opencode"
    resp.OpencodeURL = "https://" + s.OpencodeSubdomainPrefix + "-" + subID + "." + domain + "/auth?token=" + authToken
}
```

- [ ] **Step 2: Update attachWeixinBindings guard**

At `attachWeixinBindings()` (line 531), change:

```go
if resp.Type != "openclaw" {
```

To:

```go
if resp.Type != "openclaw" && resp.Type != "nanoclaw" {
```

- [ ] **Step 3: Update handleWeixinQRStart guard**

At line 1636, change:

```go
if sbx.Type != "openclaw" {
    http.Error(w, "weixin login is only available for openclaw sandboxes", http.StatusBadRequest)
```

To:

```go
if sbx.Type != "openclaw" && sbx.Type != "nanoclaw" {
    http.Error(w, "weixin login is only available for openclaw and nanoclaw sandboxes", http.StatusBadRequest)
```

- [ ] **Step 4: Update handleWeixinQRWait guard**

At line 1675, same change:

```go
if sbx.Type != "openclaw" && sbx.Type != "nanoclaw" {
    http.Error(w, "weixin login is only available for openclaw and nanoclaw sandboxes", http.StatusBadRequest)
```

- [ ] **Step 5: Update saveWeixinCredentials for nanoclaw**

At `saveWeixinCredentials()` (line 1750), the function currently writes credentials into the pod filesystem (openclaw-specific). For nanoclaw, credentials should be saved to DB instead.

**Important restructuring:** The existing function starts with `commander, ok := s.ProcessManager.(execCommander)` which is only needed for openclaw. We need to:
1. Move `accountID` extraction before the commander check (it's needed for both paths)
2. Add a sandbox lookup and type branch before the commander check
3. Keep the existing openclaw logic unchanged after the branch

Rewrite the function as:

```go
func (s *Server) saveWeixinCredentials(ctx context.Context, sandboxID string, result *weixin.StatusResult) error {
	accountID := normalizeAccountID(result.BotID)
	if accountID == "" {
		return fmt.Errorf("empty bot ID from ilink response")
	}

	// For nanoclaw: store credentials in DB (bridge mode).
	sbx, ok := s.Sandboxes.Get(sandboxID)
	if ok && sbx.Type == "nanoclaw" {
		// Phase 3 will add SaveBotCredentials call here.
		// For now, just save the binding record.
		if dbErr := s.DB.CreateWeixinBinding(sandboxID, accountID, result.UserID); dbErr != nil {
			log.Printf("weixin: failed to save binding record: %v", dbErr)
		}
		return nil
	}

	// Existing openclaw logic: write credentials into pod filesystem.
	commander, cmdOk := s.ProcessManager.(execCommander)
	if !cmdOk {
		return fmt.Errorf("process manager does not support exec")
	}

	// ... rest of existing openclaw code unchanged, but note:
	// - Remove the duplicate accountID/normalizeAccountID lines (already done above)
	// - Keep baseURL, credJSON, b64Cred, script, ExecSimple, CreateWeixinBinding as-is
```

The key changes are:
- `accountID` extraction moves before the type branch (was at line 1756, now at the top)
- New `Sandboxes.Get()` call is added to check sandbox type
- The `commander` type assertion is renamed to `cmdOk` to avoid shadowing the outer `ok`
- The rest of the existing openclaw logic remains unchanged

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): update guards and responses for nanoclaw type"
```

---

### Task 8: Create Dockerfile.nanoclaw and Entrypoint

**Files:**
- Create: `Dockerfile.nanoclaw`
- Create: `nanoclaw-entrypoint.sh`

- [ ] **Step 1: Create entrypoint script**

Create `nanoclaw-entrypoint.sh`:

```bash
#!/bin/sh
# Write .env from NANOCLAW_CONFIG_CONTENT environment variable.
# Same pattern as openclaw config injection via shell heredoc.
if [ -n "$NANOCLAW_CONFIG_CONTENT" ]; then
    echo "$NANOCLAW_CONFIG_CONTENT" > /app/.env
fi
exec "$@"
```

- [ ] **Step 2: Create Dockerfile**

Create `Dockerfile.nanoclaw`:

```dockerfile
ARG NANOCLAW_VERSION=main
FROM node:20-slim AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y git python3 make g++ && rm -rf /var/lib/apt/lists/*

# Clone NanoClaw source at pinned version
ARG NANOCLAW_VERSION
RUN git clone --branch ${NANOCLAW_VERSION} --depth 1 \
    https://github.com/qwibitai/nanoclaw.git . && \
    npm ci && npm run build

FROM node:20-slim

WORKDIR /app

# Install Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# Copy built NanoClaw
COPY --from=builder /app /app

# NanoClaw data directories
RUN mkdir -p /app/store /app/groups /app/data

# Config injection entrypoint
COPY nanoclaw-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 3002

ENTRYPOINT ["/entrypoint.sh"]
CMD ["node", "dist/index.js"]
```

Note: Weixin channel and process-runner patches are added in Phase 3. This Dockerfile creates a working baseline NanoClaw image.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile.nanoclaw nanoclaw-entrypoint.sh
git commit -m "feat: add Dockerfile.nanoclaw and entrypoint script"
```

---

### Task 9: Frontend — Add NanoClaw to CreateSandboxModal

**Files:**
- Modify: `web/src/components/CreateSandboxModal.tsx`

- [ ] **Step 1: Update onCreate type**

At the `CreateSandboxModalProps` interface (line 8), update:

```typescript
onCreate: (name: string, type: 'opencode' | 'openclaw' | 'nanoclaw', cpu?: number, memory?: number, idleTimeout?: number) => void
```

- [ ] **Step 2: Add NanoClaw type button**

Find the type selection buttons (lines ~122-148). After the OpenClaw button, add a NanoClaw button following the same pattern:

```tsx
<button
  type="button"
  className={`...${sandboxType === 'nanoclaw' ? ' selected-class' : ''}`}
  onClick={() => setSandboxType('nanoclaw')}
>
  NanoClaw
</button>
```

Match the exact className pattern used by the existing buttons.

- [ ] **Step 3: Update state initial type**

Ensure `sandboxType` state can hold `'nanoclaw'`:

```typescript
const [sandboxType, setSandboxType] = useState<'opencode' | 'openclaw' | 'nanoclaw'>('opencode')
```

- [ ] **Step 4: Commit**

```bash
git add web/src/components/CreateSandboxModal.tsx
git commit -m "feat(web): add NanoClaw type to sandbox creation form"
```

---

### Task 10: Frontend — Update SandboxDetail for NanoClaw

**Files:**
- Modify: `web/src/components/SandboxDetail.tsx`

- [ ] **Step 1: Add isNanoClaw constant**

After `const isOpenClaw = sandbox.type === 'openclaw'` (line 166), add:

```typescript
const isNanoClaw = sandbox.type === 'nanoclaw'
```

- [ ] **Step 2: Update WeChat bindings useEffect**

At lines 149-151, change:

```typescript
if (sandbox.type === 'openclaw') {
```

To:

```typescript
if (sandbox.type === 'openclaw' || sandbox.type === 'nanoclaw') {
```

- [ ] **Step 3: Update WeChat button visibility**

At line 240, change:

```typescript
{isOpenClaw && isRunning && (
```

To:

```typescript
{(isOpenClaw || isNanoClaw) && isRunning && (
```

- [ ] **Step 4: Update sandbox URL**

At line 167, change:

```typescript
const sandboxUrl = isOpenClaw ? sandbox.openclaw_url : sandbox.opencode_url
```

To:

```typescript
const sandboxUrl = isOpenClaw ? sandbox.openclaw_url : isNanoClaw ? null : sandbox.opencode_url
```

And hide the "Open" button when `sandboxUrl` is null (nanoclaw has no Web UI).

- [ ] **Step 5: Update WeChat bindings display**

At lines ~448-470, change the guard from `isOpenClaw` to:

```typescript
{(isOpenClaw || isNanoClaw) && weixinBindings.length > 0 && (
```

- [ ] **Step 6: Commit**

```bash
git add web/src/components/SandboxDetail.tsx
git commit -m "feat(web): update SandboxDetail for nanoclaw type support"
```

---

### Task 11: Verify Phase 1 Build

- [ ] **Step 1: Build backend**

Run: `cd /root/agentserver && go build ./...`
Expected: No errors

- [ ] **Step 2: Run config tests**

Run: `cd /root/agentserver && go test ./internal/sandbox/ -v`
Expected: All tests pass

- [ ] **Step 3: Build frontend**

Run: `cd /root/agentserver/web && npm run build`
Expected: No errors (or check if there's a different build command)

- [ ] **Step 4: Commit any fixes**

If any build issues found, fix and commit.

---

## Phase 3: WeChat Message Bridge

> **Investigation complete.** See `docs/superpowers/specs/2026-03-25-ilink-api-investigation.md`.
>
> Key finding: iLink uses **long-polling** (not webhooks). agentserver must run a per-sandbox
> polling goroutine that calls `getUpdates` in a loop, then forwards messages to the NanoClaw pod.
>
> Tasks 12-14 are already implemented. Tasks 15-21 are the remaining work.

### Task 12: DB Migration for WeChat Bridge Credentials — DONE

Already implemented (commit `acc6e9d`).

### Task 13: DB Functions for Bridge Credential Lookup — DONE

Already implemented (commit `749082d`).

### Task 14: Update saveWeixinCredentials for NanoClaw Bridge Mode — DONE

Already implemented (commit `d344433`).

---

### Task 15: DB Migration for Polling State and Context Tokens

**Files:**
- Modify: `internal/db/migrations/009_nanoclaw_weixin_bridge.sql` (append to existing)

The long-polling model requires persisting:
1. `get_updates_buf` — cursor for resuming long-poll after restart
2. `context_token` — per-user session token that must be echoed in replies

- [ ] **Step 1: Update migration 009 to include polling state columns**

Append to the existing `009_nanoclaw_weixin_bridge.sql`:

```sql
-- Polling state for long-poll bridge mode.
-- get_updates_buf is the cursor returned by iLink getUpdates, persisted for restart recovery.
ALTER TABLE sandbox_weixin_bindings ADD COLUMN get_updates_buf TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN last_poll_at TIMESTAMPTZ;

-- Context token store: iLink requires echoing context_token on every outbound message.
-- One token per sandbox+bot+user triple, updated on each inbound message.
CREATE TABLE IF NOT EXISTS weixin_context_tokens (
    sandbox_id TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    bot_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    context_token TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (sandbox_id, bot_id, user_id)
);
```

- [ ] **Step 2: Add DB functions for polling state and context tokens**

In `internal/db/weixin_bindings.go`, add:

```go
// UpdateGetUpdatesBuf persists the long-poll cursor for a binding.
func (db *DB) UpdateGetUpdatesBuf(sandboxID, botID, buf string) error {
	_, err := db.Exec(
		`UPDATE sandbox_weixin_bindings SET get_updates_buf = $1, last_poll_at = NOW()
		 WHERE sandbox_id = $2 AND bot_id = $3`,
		buf, sandboxID, botID,
	)
	return err
}

// GetBindingsWithBotToken returns all nanoclaw bindings that have a bot_token (for starting pollers).
func (db *DB) GetBindingsWithBotToken() ([]*WeixinBinding, error) {
	rows, err := db.Query(
		`SELECT b.id, b.sandbox_id, b.bot_id, b.user_id, b.bound_at, b.bot_token, b.ilink_base_url, b.get_updates_buf
		 FROM sandbox_weixin_bindings b
		 JOIN sandboxes s ON s.id = b.sandbox_id
		 WHERE b.bot_token IS NOT NULL AND s.type = 'nanoclaw' AND s.status = 'running'`)
	// ... scan and return
}

// UpsertContextToken stores or updates the context_token for a user conversation.
func (db *DB) UpsertContextToken(sandboxID, botID, userID, contextToken string) error {
	_, err := db.Exec(
		`INSERT INTO weixin_context_tokens (sandbox_id, bot_id, user_id, context_token, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (sandbox_id, bot_id, user_id) DO UPDATE SET context_token = $4, updated_at = NOW()`,
		sandboxID, botID, userID, contextToken,
	)
	return err
}

// GetContextToken retrieves the cached context_token for a user.
func (db *DB) GetContextToken(sandboxID, botID, userID string) (string, error) {
	var token string
	err := db.QueryRow(
		`SELECT context_token FROM weixin_context_tokens WHERE sandbox_id = $1 AND bot_id = $2 AND user_id = $3`,
		sandboxID, botID, userID,
	).Scan(&token)
	if err != nil {
		return "", err
	}
	return token, nil
}
```

Also add new fields to `WeixinBinding` struct:

```go
type WeixinBinding struct {
	ID            int
	SandboxID     string
	BotID         string
	UserID        string
	BoundAt       time.Time
	BotToken      string  // iLink bot auth token
	ILinkBaseURL  string  // iLink API base URL
	GetUpdatesBuf string  // long-poll cursor
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/009_nanoclaw_weixin_bridge.sql internal/db/weixin_bindings.go
git commit -m "feat(db): add polling state and context token storage for weixin bridge"
```

---

### Task 16: iLink Message API Client

**Files:**
- Modify: `internal/weixin/ilink.go`
- Create: `internal/weixin/ilink_test.go`

Implement the iLink getUpdates and sendMessage APIs in Go, based on the
`@tencent-weixin/openclaw-weixin` source (see `api/api.ts` and `api/types.ts`).

- [ ] **Step 1: Add message types**

```go
// WeixinMessage is a message from iLink getUpdates.
type WeixinMessage struct {
	Seq           int              `json:"seq,omitempty"`
	MessageID     int              `json:"message_id,omitempty"`
	FromUserID    string           `json:"from_user_id,omitempty"`
	ToUserID      string           `json:"to_user_id,omitempty"`
	ClientID      string           `json:"client_id,omitempty"`
	CreateTimeMs  int64            `json:"create_time_ms,omitempty"`
	SessionID     string           `json:"session_id,omitempty"`
	MessageType   int              `json:"message_type,omitempty"`   // 1=USER, 2=BOT
	MessageState  int              `json:"message_state,omitempty"`  // 0=NEW, 1=GENERATING, 2=FINISH
	ItemList      []MessageItem    `json:"item_list,omitempty"`
	ContextToken  string           `json:"context_token,omitempty"`
}

type MessageItem struct {
	Type     int       `json:"type,omitempty"`  // 1=TEXT, 2=IMAGE, 3=VOICE, 4=FILE, 5=VIDEO
	TextItem *TextItem `json:"text_item,omitempty"`
}

type TextItem struct {
	Text string `json:"text,omitempty"`
}

type GetUpdatesRequest struct {
	GetUpdatesBuf string   `json:"get_updates_buf"`
	BaseInfo      BaseInfo `json:"base_info"`
}

type GetUpdatesResponse struct {
	Ret                  int              `json:"ret"`
	ErrCode              int              `json:"errcode,omitempty"`
	ErrMsg               string           `json:"errmsg,omitempty"`
	Msgs                 []WeixinMessage  `json:"msgs,omitempty"`
	GetUpdatesBuf        string           `json:"get_updates_buf,omitempty"`
	LongPollingTimeoutMs int              `json:"longpolling_timeout_ms,omitempty"`
}

type SendMessageRequest struct {
	Msg      WeixinMessage `json:"msg"`
	BaseInfo BaseInfo      `json:"base_info"`
}

type BaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
}
```

- [ ] **Step 2: Add GetUpdates function**

```go
const (
	defaultLongPollTimeout = 35 * time.Second
	defaultAPITimeout      = 15 * time.Second
	channelVersion         = "agentserver-bridge-1.0"
)

// GetUpdates long-polls iLink for new messages. Returns empty response on client timeout.
func GetUpdates(ctx context.Context, apiBaseURL, botToken, getUpdatesBuf string) (*GetUpdatesResponse, error) {
	body := GetUpdatesRequest{
		GetUpdatesBuf: getUpdatesBuf,
		BaseInfo:      BaseInfo{ChannelVersion: channelVersion},
	}
	// POST to {baseUrl}/ilink/bot/getupdates
	// Headers: Authorization: Bearer {botToken}, AuthorizationType: ilink_bot_token
	// Timeout: 35s (server holds request)
	// On client timeout: return empty {Ret:0, Msgs:[]}
}
```

- [ ] **Step 3: Add SendMessage function**

```go
// SendTextMessage sends a text message to a WeChat user via iLink.
func SendTextMessage(ctx context.Context, apiBaseURL, botToken, toUserID, text, contextToken string) error {
	clientID := fmt.Sprintf("agentserver-%d", time.Now().UnixMilli())
	body := SendMessageRequest{
		Msg: WeixinMessage{
			ToUserID:     toUserID,
			ClientID:     clientID,
			MessageType:  2, // BOT
			MessageState: 2, // FINISH
			ContextToken: contextToken,
			ItemList: []MessageItem{{
				Type:     1, // TEXT
				TextItem: &TextItem{Text: text},
			}},
		},
		BaseInfo: BaseInfo{ChannelVersion: channelVersion},
	}
	// POST to {baseUrl}/ilink/bot/sendmessage
	// Headers: Authorization: Bearer {botToken}, AuthorizationType: ilink_bot_token
}
```

- [ ] **Step 4: Write tests for request building**

Test that `GetUpdatesRequest` and `SendMessageRequest` serialize correctly.

- [ ] **Step 5: Commit**

```bash
git add internal/weixin/ilink.go internal/weixin/ilink_test.go
git commit -m "feat(weixin): add iLink getUpdates and sendMessage API client"
```

---

### Task 17: Per-Sandbox Polling Bridge

**Files:**
- Create: `internal/weixin/bridge.go`
- Modify: `internal/server/server.go`

This is the core of the bridge: a goroutine per nanoclaw sandbox that long-polls iLink
for messages and forwards them to the NanoClaw pod.

- [ ] **Step 1: Create bridge.go with the polling loop**

```go
package weixin

// Bridge manages per-sandbox long-poll goroutines for nanoclaw WeChat bindings.
type Bridge struct {
	db       BridgeDB       // interface for DB operations
	pollers  map[string]context.CancelFunc  // sandboxID+botID → cancel
	mu       sync.Mutex
}

// BridgeDB is the DB interface needed by the bridge.
type BridgeDB interface {
	GetBindingsWithBotToken() ([]*db.WeixinBinding, error)
	UpdateGetUpdatesBuf(sandboxID, botID, buf string) error
	UpsertContextToken(sandboxID, botID, userID, token string) error
	GetContextToken(sandboxID, botID, userID string) (string, error)
}

// StartPoller starts a long-poll goroutine for a single binding.
func (b *Bridge) StartPoller(binding *db.WeixinBinding, podIP string) {
	// goroutine loop:
	//   1. Call GetUpdates(baseURL, botToken, getUpdatesBuf)
	//   2. For each message:
	//      a. Extract context_token → UpsertContextToken
	//      b. Extract text from item_list
	//      c. POST to NanoClaw pod: http://{podIP}:3002/message
	//   3. Save new get_updates_buf → UpdateGetUpdatesBuf
	//   4. Handle errors: retry with backoff, session expired pause
}

// StopPoller stops the polling goroutine for a binding.
func (b *Bridge) StopPoller(sandboxID, botID string)

// RestorePollers starts pollers for all active nanoclaw WeChat bindings.
// Called on agentserver startup.
func (b *Bridge) RestorePollers()
```

- [ ] **Step 2: Add outbound bridge endpoint to server.go**

```go
// POST /api/internal/nanoclaw/{id}/weixin/send
// Called by NanoClaw pod's weixin channel to send a reply.
// Authenticated via nanoclaw bridge secret.
func (s *Server) handleNanoclawWeixinSend(w http.ResponseWriter, r *http.Request) {
	sandboxID := chi.URLParam(r, "id")
	// Validate bridge secret from Authorization header
	// Parse body: { bot_id, to_user_id, text }
	// Look up context_token from DB
	// Call SendTextMessage(baseURL, botToken, toUserID, text, contextToken)
}
```

Register route:
```go
r.Post("/api/internal/nanoclaw/{id}/weixin/send", s.handleNanoclawWeixinSend)
```

- [ ] **Step 3: Start bridge on sandbox start, stop on sandbox stop**

In the sandbox creation flow (after pod is running), if weixin is bound:
```go
if sandboxType == "nanoclaw" {
	s.WeixinBridge.StartPollersForSandbox(id, podIP)
}
```

In sandbox stop/delete, stop the pollers:
```go
s.WeixinBridge.StopPollersForSandbox(id)
```

- [ ] **Step 4: Start polling after QR scan confirmation**

In `saveWeixinCredentials` nanoclaw branch, after saving credentials:
```go
// Start polling for this newly bound account.
sbx, _ := s.Sandboxes.Get(sandboxID)
if sbx != nil && sbx.PodIP != "" {
    s.WeixinBridge.StartPoller(binding, sbx.PodIP)
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/weixin/bridge.go internal/server/server.go
git commit -m "feat(weixin): add per-sandbox polling bridge for nanoclaw"
```

---

### Task 18: NanoClaw Weixin Channel Implementation

**Files:**
- Create: `nanoclaw-weixin-channel/index.ts`

This is the TypeScript channel implementation that runs inside the NanoClaw container.
It receives messages from agentserver via HTTP and sends replies back via HTTP callback.

- [ ] **Step 1: Create the channel implementation**

```typescript
// nanoclaw-weixin-channel/index.ts
import { registerChannel } from '../registry.js';
import type { Channel, ChannelOpts, NewMessage } from '../../types.js';
import http from 'http';
import https from 'https';

class WeixinChannel implements Channel {
    name = 'weixin';
    private server: http.Server;
    private opts: ChannelOpts;
    private bridgeURL: string;
    private bridgeSecret: string;
    private connected = false;

    constructor(opts: ChannelOpts, bridgeURL: string, bridgeSecret: string) {
        this.opts = opts;
        this.bridgeURL = bridgeURL;
        this.bridgeSecret = bridgeSecret;

        this.server = http.createServer(async (req, res) => {
            // Auth check
            if (req.headers['authorization'] !== `Bearer ${this.bridgeSecret}`) {
                res.writeHead(401); res.end('Unauthorized'); return;
            }
            if (req.method === 'GET' && req.url === '/health') {
                res.writeHead(200); res.end('ok'); return;
            }
            if (req.method === 'POST' && req.url === '/message') {
                const body = await readBody(req);
                const msg: NewMessage = JSON.parse(body);
                this.opts.onMessage(msg.chat_jid, msg);
                res.writeHead(200); res.end('ok');
                return;
            }
            res.writeHead(404); res.end();
        });
    }

    async connect() {
        await new Promise<void>(r => this.server.listen(3002, '0.0.0.0', r));
        this.connected = true;
    }

    async sendMessage(jid: string, text: string) {
        // POST to bridgeURL with {bot_id, to_user_id: jid, text}
        // agentserver forwards to iLink sendmessage
    }

    isConnected() { return this.connected; }
    ownsJid(jid: string) { return jid.endsWith('@im.wechat'); }
    async disconnect() { this.server.close(); }
}

registerChannel('weixin', (opts) => {
    const bridgeURL = process.env.NANOCLAW_WEIXIN_BRIDGE_URL;
    const bridgeSecret = process.env.NANOCLAW_BRIDGE_SECRET;
    if (!bridgeURL || !bridgeSecret) return null;
    return new WeixinChannel(opts, bridgeURL, bridgeSecret);
});
```

- [ ] **Step 2: Commit**

```bash
git add nanoclaw-weixin-channel/
git commit -m "feat: add NanoClaw weixin channel implementation"
```

---

### Task 19: Update Dockerfile.nanoclaw for Weixin Channel

**Files:**
- Modify: `Dockerfile.nanoclaw`

- [ ] **Step 1: Add weixin channel copy and barrel import to Dockerfile**

After the NanoClaw source is cloned and built, add:

```dockerfile
# Copy weixin channel implementation
COPY nanoclaw-weixin-channel/ src/channels/weixin/

# Register weixin channel in barrel import
RUN echo 'import "./weixin/index.js";' >> src/channels/index.ts

# Rebuild with weixin channel
RUN npm run build
```

- [ ] **Step 2: Commit**

```bash
git add Dockerfile.nanoclaw
git commit -m "feat: add weixin channel to Dockerfile.nanoclaw"
```

---

### Task 20: NanoClaw Process Runner Patch

**Files:**
- Create: `nanoclaw-patches/process-runner.ts`
- Modify: `Dockerfile.nanoclaw`

When `NANOCLAW_NO_CONTAINER=true`, NanoClaw's container-runner cannot spawn Docker containers.
This patch adds a process-based fallback that spawns agent-runner as a child process.

- [ ] **Step 1: Create process-runner patch**

```typescript
// nanoclaw-patches/process-runner.ts
// Replaces Docker-based agent execution when NANOCLAW_NO_CONTAINER=true.
// Spawns agent-runner directly as a child process.
// K8s Pod provides isolation (one NanoClaw instance per sandbox).
```

Key changes:
- Check `process.env.NANOCLAW_NO_CONTAINER` at top of `runContainerAgent()`
- If set, spawn `node agent-runner/dist/index.js` directly via `child_process.spawn()`
- Pass input via stdin JSON, read output markers from stdout
- Skip Docker volume mounts, container naming, credential proxy setup
- Environment variables (ANTHROPIC_BASE_URL, etc.) are already set at Pod level

- [ ] **Step 2: Add to Dockerfile**

```dockerfile
COPY nanoclaw-patches/process-runner.ts src/process-runner.ts
```

- [ ] **Step 3: Commit**

```bash
git add nanoclaw-patches/ Dockerfile.nanoclaw
git commit -m "feat: add process-runner patch for NanoClaw no-container mode"
```

---

### Task 21: Phase 3 Build Verification and Integration Test

- [ ] **Step 1: Build backend**

Run: `cd /root/agentserver && go build ./...`

- [ ] **Step 2: Run all tests**

Run: `cd /root/agentserver && go test ./...`

- [ ] **Step 3: Build Dockerfile.nanoclaw**

Run: `docker build -f Dockerfile.nanoclaw -t nanoclaw-test:latest .`
(May need Docker available; if not, verify Dockerfile syntax only)

- [ ] **Step 4: Commit any fixes**
