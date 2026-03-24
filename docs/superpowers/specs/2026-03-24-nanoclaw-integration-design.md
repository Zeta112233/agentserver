# NanoClaw Integration Design Spec

## Overview

Add NanoClaw as a third sandbox type in agentserver, alongside opencode and openclaw. NanoClaw is a lightweight AI assistant platform built on Claude Agent SDK with multi-channel messaging support (WhatsApp, Telegram, Discord, Slack, etc.).

The integration reuses agentserver's existing sandbox lifecycle management, llmproxy for API key injection, and iLink backend for WeChat support. WeChat messages are bridged through agentserver rather than having NanoClaw connect directly to iLink.

**Source:** https://github.com/qwibitai/nanoclaw

## Architecture

```
┌──────────────────────────────────────────────────┐
│                  agentserver                      │
│                                                   │
│  ┌─────────┐  ┌──────────┐  ┌─────────────────┐ │
│  │ Web UI  │  │ llmproxy │  │ iLink Backend   │ │
│  │ (React) │  │ (rate    │  │ (WeChat QR +    │ │
│  │         │  │  limit)  │  │  msg bridge)    │ │
│  └────┬────┘  └────┬─────┘  └───────┬─────────┘ │
│       │            │                │             │
│       ▼            ▼                ▼             │
│  ┌─────────────────────────────────────────────┐ │
│  │          Sandbox Manager (K8s)               │ │
│  │  type: "opencode" | "openclaw" | "nanoclaw" │ │
│  └──────────────────┬──────────────────────────┘ │
└─────────────────────┼────────────────────────────┘
                      │
          ┌───────────┼───────────┐
          ▼           ▼           ▼
    ┌──────────┐ ┌──────────┐ ┌──────────────┐
    │ opencode │ │ openclaw │ │  nanoclaw    │
    │ Pod      │ │ Pod      │ │  Pod         │
    │          │ │          │ │              │
    │ opencode │ │ openclaw │ │ NanoClaw     │
    │ binary   │ │ gateway  │ │ orchestrator │
    │          │ │ + plugins│ │ + channels   │
    └──────────┘ └──────────┘ │ + Agent SDK  │
                               └──────────────┘
```

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Integration level | Full (same as openclaw) | User requirement |
| Web UI proxy | Not needed | NanoClaw interacts via messaging channels, no Web UI |
| Agent execution | Direct Claude Agent SDK | K8s Pod provides isolation; no Docker daemon available inside Pod |
| API key injection | Via llmproxy | Consistent with openclaw; rate limiting and key management |
| Container image | Project-provided Dockerfile.nanoclaw | Pre-installs weixin channel and dependencies |
| WeChat messages | Bridged via agentserver | Centralizes iLink credential management; NanoClaw doesn't hold iLink secrets |
| BYOK support | Yes, same as openclaw | Consistent user experience across sandbox types |

## Components

### 1. Sandbox Type System Extension

**File: `internal/sandbox/config.go`**

Add new config fields:

```go
type Config struct {
    // ... existing fields ...
    NanoclawImage            string
    NanoclawRuntimeClassName string
    NanoclawWeixinEnabled    bool
}
```

New environment variables:
- `NANOCLAW_IMAGE` — container image for NanoClaw pods
- `NANOCLAW_RUNTIME_CLASS` — optional K8s runtime class
- `NANOCLAW_WEIXIN_ENABLED` — enable WeChat channel in NanoClaw pods

Add `BuildNanoclawConfig()` function that generates NanoClaw's environment variables as a key=value string (NanoClaw reads config from environment variables and `.env` file via `readEnvFile()` in `src/env.ts`):

```go
func BuildNanoclawConfig(proxyBaseURL, proxyToken, assistantName string, weixinBridgeURL string, bridgeSecret string, byokBaseURL, byokAPIKey string) string {
    // Lines written to .env file, injected via NANOCLAW_CONFIG_CONTENT env var
    // and written to /app/.env at container startup by entrypoint script.
    //
    // ANTHROPIC_BASE_URL=<llmproxy URL or byokBaseURL>
    // ANTHROPIC_API_KEY=<per-sandbox proxy token or byokAPIKey>
    // ASSISTANT_NAME=<configurable, default "Andy">
    // NANOCLAW_NO_CONTAINER=true
    // NANOCLAW_WEIXIN_BRIDGE_URL=<agentserver webhook URL>  (if weixin enabled)
    // NANOCLAW_BRIDGE_SECRET=<shared secret for HTTP auth>  (if weixin enabled)
}
```

**File: `internal/server/server.go`**

- Extend sandbox type validation: `"opencode" | "openclaw" | "nanoclaw"`
- Add nanoclaw branch in sandbox creation logic
- Route weixin handlers to work with nanoclaw sandboxes:
  - `handleWeixinQRStart` (line ~1607): change `sbx.Type != "openclaw"` guard to accept `"nanoclaw"`
  - `handleWeixinQRWait` (line ~1646): same guard update
  - `attachWeixinBindings` (line ~502): change filter to accept both `"openclaw"` and `"nanoclaw"`
- `toSandboxResponse` (line ~439): add `"nanoclaw"` case — no URL generation (no Web UI)
- BYOK handling: apply same BYOK override logic as openclaw (lines ~1222-1229)

### 2. Dockerfile.nanoclaw

```dockerfile
FROM node:20-slim AS builder

ARG NANOCLAW_VERSION=main

WORKDIR /app

# Install Claude Code CLI (for Agent SDK)
RUN npm install -g @anthropic-ai/claude-code

# Clone NanoClaw source at pinned version
RUN apt-get update && apt-get install -y git && \
    git clone --branch ${NANOCLAW_VERSION} --depth 1 \
    https://github.com/qwibitai/nanoclaw.git . && \
    npm ci && npm run build

# Copy weixin channel implementation
COPY nanoclaw-weixin-channel/ src/channels/weixin/

# Copy no-container mode adapter
COPY nanoclaw-patches/process-runner.ts src/process-runner.ts

# Update channels barrel to include weixin
RUN echo 'import "./weixin/index.js";' >> src/channels/index.ts

# Rebuild with weixin channel and process-runner
RUN npm run build

FROM node:20-slim

WORKDIR /app
COPY --from=builder /app /app
COPY --from=builder /usr/local/lib/node_modules/@anthropic-ai /usr/local/lib/node_modules/@anthropic-ai
COPY --from=builder /usr/local/bin/claude /usr/local/bin/claude

# NanoClaw data directories
RUN mkdir -p /app/store /app/groups /app/data

# Entrypoint writes .env from NANOCLAW_CONFIG_CONTENT
COPY nanoclaw-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Health check endpoint port
EXPOSE 3002

ENTRYPOINT ["/entrypoint.sh"]
CMD ["node", "dist/index.js"]
```

**`nanoclaw-entrypoint.sh`:**
```bash
#!/bin/sh
# Write .env from environment variable (same pattern as openclaw config injection)
if [ -n "$NANOCLAW_CONFIG_CONTENT" ]; then
    echo "$NANOCLAW_CONFIG_CONTENT" > /app/.env
fi
exec "$@"
```

The `NANOCLAW_VERSION` build arg allows pinning to a specific tag or commit SHA for reproducible builds. Default is `main` for development builds.

### 3. Agent Execution Adaptation

NanoClaw's `container-runner.ts` spawns Docker containers to run agents. In K8s Pod mode (no Docker daemon), this must be adapted.

**Approach: Runtime-selected process runner (maintained patch)**

Create `nanoclaw-patches/process-runner.ts` — a modified version of NanoClaw's `runContainerAgent()` that executes the agent-runner as a direct child process:

```typescript
// process-runner.ts — drop-in replacement for container-runner's runContainerAgent
// when NANOCLAW_NO_CONTAINER=true
//
// Key differences from container mode:
// - Spawns agent-runner directly via child_process.spawn('node', ['agent-runner/dist/index.js'])
// - No Docker volume mounts — groups folder is directly accessible on filesystem
// - No credential-proxy indirection — ANTHROPIC_BASE_URL already points to llmproxy
// - No per-agent container isolation — K8s Pod provides the isolation boundary
//
// Trade-offs:
// - All groups share the same process namespace and filesystem within the Pod
// - Resource limits per agent are not enforced (Pod-level limits apply to all)
// - This is acceptable because each NanoClaw sandbox is its own Pod
```

The `container-runner.ts` is modified to check `process.env.NANOCLAW_NO_CONTAINER` at the top of `runContainerAgent()` and delegate to the process-runner when set. This is a maintained fork patch, not an upstream contribution, because NanoClaw's Docker-based isolation is correct for its intended use case (single-user deployment).

### 4. WeChat Channel for NanoClaw

A new channel implementation within the NanoClaw container that receives/sends WeChat messages via agentserver as a bridge.

**File: `nanoclaw-weixin-channel/index.ts`** (copied into container at build time)

```typescript
import { registerChannel } from '../registry.js';
import { Channel, ChannelOpts, NewMessage } from '../../types.js';
import http from 'http';

class WeixinChannel implements Channel {
    name = 'weixin';
    private server: http.Server;
    private opts: ChannelOpts;
    private bridgeURL: string;  // agentserver webhook URL for outbound messages
    private bridgeSecret: string; // shared secret for request authentication
    private connected = false;

    constructor(opts: ChannelOpts, bridgeURL: string, bridgeSecret: string) {
        this.opts = opts;
        this.bridgeURL = bridgeURL;
        this.bridgeSecret = bridgeSecret;

        this.server = http.createServer((req, res) => {
            // Validate shared secret on all inbound requests
            const authHeader = req.headers['authorization'];
            if (authHeader !== `Bearer ${this.bridgeSecret}`) {
                res.writeHead(401);
                res.end('Unauthorized');
                return;
            }

            if (req.method === 'POST' && req.url === '/message') {
                // Receive message from agentserver bridge
                // Parse JSON body: { id, chat_jid, sender, sender_name, content, timestamp }
                // Call opts.onMessage(chat_jid, message)
            }

            if (req.method === 'POST' && req.url === '/metadata') {
                // Receive chat metadata updates
                // Call opts.onChatMetadata(chat_jid, timestamp, name, 'weixin', isGroup)
            }

            if (req.method === 'GET' && req.url === '/health') {
                // Health check endpoint for K8s readiness probe
                res.writeHead(200);
                res.end('ok');
            }
        });
    }

    async connect(): Promise<void> {
        await new Promise<void>(resolve => this.server.listen(3002, '0.0.0.0', resolve));
        this.connected = true;
    }

    async sendMessage(jid: string, text: string): Promise<void> {
        // HTTP POST to agentserver bridge URL
        // Headers: { 'Authorization': `Bearer ${this.bridgeSecret}`, 'Content-Type': 'application/json' }
        // Body: { jid, text }
        // agentserver forwards to iLink API → WeChat user
    }

    isConnected(): boolean { return this.connected; }
    ownsJid(jid: string): boolean { return jid.startsWith('weixin:'); }
    async disconnect(): Promise<void> { this.server.close(); }
}

registerChannel('weixin', (opts) => {
    const bridgeURL = process.env.NANOCLAW_WEIXIN_BRIDGE_URL;
    const bridgeSecret = process.env.NANOCLAW_BRIDGE_SECRET;
    if (!bridgeURL || !bridgeSecret) return null;
    return new WeixinChannel(opts, bridgeURL, bridgeSecret);
});
```

**Authentication:** All HTTP communication between agentserver and NanoClaw pod uses a shared secret (`NANOCLAW_BRIDGE_SECRET`) as a Bearer token. This secret is generated per-sandbox at creation time, stored in the sandbox DB row, and injected into the Pod via environment variable.

**Health check:** The `/health` endpoint on port 3002 serves as the K8s readiness probe target. This endpoint is always available when the weixin channel is enabled. When weixin is not enabled (no `NANOCLAW_WEIXIN_BRIDGE_URL`), a separate minimal health HTTP server should be started on port 3002 to support the readiness probe.

### 5. WeChat Message Bridge (agentserver side)

**Important: iLink message API is new development.**

The existing `internal/weixin/ilink.go` only implements QR code generation (`StartLogin`) and scan status polling (`PollLoginStatus`). It does NOT have message send/receive capability. The bridge requires implementing new iLink API integration:

**New functions in `internal/weixin/ilink.go`:**

```go
// SendMessage sends a text message to a WeChat user via iLink API.
// This is new functionality — the iLink message send API format needs to be
// confirmed with iLink documentation.
func (c *Client) SendMessage(botToken, userID, content string) error

// RegisterWebhook registers agentserver as the webhook receiver for a bot.
// Called after successful QR login to set up inbound message delivery.
// If iLink does not support webhook push, this will need to become a
// polling loop instead (see "iLink API investigation" below).
func (c *Client) RegisterWebhook(botToken, webhookURL string) error
```

**iLink API investigation required before implementation:**
The current iLink integration only uses the QR scan flow. For the message bridge to work, we need to confirm:
1. Does iLink support push/webhook delivery of inbound messages? Or polling only?
2. What is the message send API format?
3. How are bot credentials (bot_token from QR scan) used for messaging?

If iLink only supports polling (no webhook push), the bridge architecture changes: agentserver would need a per-sandbox polling loop that checks iLink for new messages and forwards them to the NanoClaw pod. This is functionally equivalent but adds polling overhead.

**Post-QR-login credential flow (differs from openclaw):**

For openclaw: `saveWeixinCredentials` writes iLink bot_token/credentials into the pod filesystem, and openclaw's plugin handles messaging directly.

For nanoclaw: After QR login succeeds:
1. Store `bot_token` and `user_id` in agentserver DB (new columns on `sandbox_weixin_bindings` or a new table)
2. Register agentserver's webhook URL with iLink for this bot (if webhook supported)
3. agentserver holds all iLink credentials — NanoClaw pod never sees them
4. Future messages flow through the agentserver bridge

**New agentserver endpoints:**

```
POST /api/sandboxes/{id}/weixin/message-callback
```
Called by iLink when a WeChat user sends a message. Agentserver looks up the sandbox by bot_id (reverse lookup, see DB changes below) and forwards to the NanoClaw pod's weixin channel endpoint.

```
POST /api/internal/nanoclaw/{id}/weixin/send
```
Called by the NanoClaw pod's weixin channel to send a reply. Authenticated via bridge secret. Agentserver looks up the bot credentials and calls iLink API to send the message.

### 6. Database Changes

The bridge model requires schema changes. New migration:

**`internal/db/migrations/006_nanoclaw_weixin_bridge.sql`:**

```sql
-- Add bridge-mode credential columns to sandbox_weixin_bindings
ALTER TABLE sandbox_weixin_bindings ADD COLUMN bot_token TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN ilink_base_url TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN webhook_registered BOOLEAN DEFAULT FALSE;

-- Index for reverse lookup: given a bot_id from iLink webhook, find the sandbox
CREATE INDEX IF NOT EXISTS idx_weixin_bindings_bot_id ON sandbox_weixin_bindings(bot_id);

-- Add nanoclaw bridge secret to sandboxes
-- (stored alongside openclaw_token which serves a similar purpose)
ALTER TABLE sandboxes ADD COLUMN nanoclaw_bridge_secret TEXT;
```

**New DB functions in `internal/db/weixin_bindings.go`:**

```go
// GetSandboxByBotID returns the sandbox_id for a given WeChat bot_id.
// Used for routing inbound iLink messages to the correct NanoClaw sandbox.
func (db *DB) GetSandboxByBotID(botID string) (string, error)

// SaveBotCredentials stores iLink bot credentials for bridge-mode messaging.
func (db *DB) SaveBotCredentials(sandboxID, botID, botToken, baseURL string) error
```

### 7. K8s Pod Management

Extend existing sandbox K8s management to handle nanoclaw pods.

**Changes in `internal/sandbox/manager.go`:**

- `runtimeClassNameFor()` (line ~730): add `"nanoclaw"` case using `NanoclawRuntimeClassName`
- Pod spec construction: new branch for nanoclaw type with:
  - Image: `NanoclawImage`
  - No gateway port exposure (no Web UI)
  - Environment variable: `NANOCLAW_CONFIG_CONTENT` (contains the full .env content)
  - PVC mount for SQLite data persistence (`/app/store`, `/app/data`, `/app/groups`)
  - Container port 3002 (weixin bridge HTTP endpoint, cluster-internal only)
- Health/readiness probe: HTTP GET `http://pod:3002/health`

**No reverse proxy needed:** Unlike openclaw which needs subdomain routing for its Control UI, nanoclaw has no Web UI. All interaction happens through messaging channels.

### 8. Frontend Changes

**Sandbox creation form (`web/src/components/`):**
- Add "NanoClaw" option to sandbox type selector
- NanoClaw-specific config: assistant name (default "Andy")

**Sandbox detail view (`SandboxDetail.tsx`):**
- Show NanoClaw status, uptime
- WeChat binding section: update guards that currently check `sandbox.type === 'openclaw'`:
  - Line ~240: `{isOpenClaw && isRunning && (...)}` → add `isNanoClaw`
  - Line ~149-151: `useEffect` for `refreshWeixinBindings` → add `nanoclaw`
- No "Open UI" button (NanoClaw has no Web UI)
- Show connected messaging channels

**API client (`api.ts`):**
- Add `sendNanoclawWeixinMessage()` for the bridge send endpoint
- Existing QR endpoints work unchanged (same API, just different sandbox type)

### 9. Existing Code Guards to Update

A systematic audit of `"openclaw"` references in the codebase reveals these guards that need to also accept `"nanoclaw"`:

| Location | Current Guard | Change |
|----------|---------------|--------|
| `server.go` ~L1607 | `sbx.Type != "openclaw"` | Accept `"nanoclaw"` |
| `server.go` ~L1646 | `sbx.Type != "openclaw"` | Accept `"nanoclaw"` |
| `server.go` ~L502 | `resp.Type != "openclaw"` | Accept `"nanoclaw"` |
| `server.go` ~L439 | `switch sbx.Type` default case | Add `"nanoclaw"` case |
| `manager.go` ~L730 | `"openclaw"` runtime class | Add `"nanoclaw"` case |
| `process.go` ~L29 | Comment: "opencode or openclaw" | Update comment |
| `SandboxDetail.tsx` ~L240 | `isOpenClaw && isRunning` | Add `isNanoClaw` |
| `SandboxDetail.tsx` ~L149 | `sandbox.type === 'openclaw'` | Add `'nanoclaw'` |

## File Changes Summary

| File | Change |
|------|--------|
| `internal/sandbox/config.go` | Add NanoClaw config fields, `BuildNanoclawConfig()` |
| `internal/server/server.go` | Add nanoclaw type validation, creation logic, weixin bridge endpoints, update openclaw guards |
| `internal/weixin/ilink.go` | Add `SendMessage()`, `RegisterWebhook()` — **new iLink API work** |
| `internal/db/weixin_bindings.go` | Add `GetSandboxByBotID()`, `SaveBotCredentials()` |
| `internal/db/migrations/006_nanoclaw_weixin_bridge.sql` | New migration — bridge credentials, reverse lookup index |
| `internal/sandbox/manager.go` | Add nanoclaw Pod spec, runtime class, health probe |
| `internal/process/process.go` | Update comment |
| `internal/sandboxproxy/` | No changes (no Web proxy for nanoclaw) |
| `Dockerfile.nanoclaw` | New file — NanoClaw container image with weixin channel |
| `nanoclaw-entrypoint.sh` | New file — config injection entrypoint |
| `nanoclaw-weixin-channel/` | New directory — weixin channel TypeScript implementation |
| `nanoclaw-patches/` | New directory — process-runner.ts for no-container mode |
| `web/src/components/SandboxDetail.tsx` | Add nanoclaw type handling, update weixin guards |
| `web/src/components/CreateSandboxModal.tsx` (or equivalent) | Add nanoclaw option |

## Risk Assessment

### High Risk: iLink Message API

The entire WeChat bridge depends on iLink's message send/receive API, which has not been used in the current codebase. If iLink does not support webhook-based message push (only polling, like the existing QR flow), the bridge needs a polling loop in agentserver. This should be the **first item investigated** before beginning implementation.

**Mitigation:** Investigate iLink message API before starting implementation. If webhook push is not available, design the polling loop as part of Section 5.

### Medium Risk: NanoClaw Version Compatibility

The Dockerfile clones NanoClaw from GitHub. NanoClaw's API and internal structure may change between versions, breaking the patches (process-runner) and channel integration.

**Mitigation:** Pin to a specific commit SHA in production builds. Monitor NanoClaw releases for breaking changes.

### Low Risk: Multi-group Isolation in Pod Mode

Without Docker containers, all NanoClaw groups within a single Pod share filesystem and process space. This is acceptable because each sandbox is its own Pod (one NanoClaw instance per user), but differs from NanoClaw's intended multi-user isolation model.

## Testing Strategy

1. **Unit tests:** `BuildNanoclawConfig()` generates correct .env content; `GetSandboxByBotID()` reverse lookup works
2. **Integration tests:** Weixin bridge endpoints forward messages correctly with auth validation
3. **Container tests:** Dockerfile.nanoclaw builds successfully, NanoClaw starts in no-container mode
4. **Auth tests:** Bridge secret validation rejects unauthorized requests
5. **E2E:** Create nanoclaw sandbox → WeChat QR bind → send/receive message

## Implementation Order

1. **Phase 0 — iLink API investigation** (blocks Phase 3)
   - Confirm iLink message send/receive API format
   - Determine webhook vs polling model
2. **Phase 1 — Sandbox type + Pod management** (no WeChat)
   - Config extension, type validation, Pod management
   - Dockerfile.nanoclaw with no-container mode
   - Frontend: creation + detail view
3. **Phase 2 — BYOK support**
   - Wire BYOK base URL and API key through to NanoClaw config
4. **Phase 3 — WeChat bridge** (depends on Phase 0 + Phase 1)
   - DB migration for bridge credentials
   - iLink message send/receive in agentserver
   - Weixin channel implementation in NanoClaw container
   - Frontend: WeChat binding for nanoclaw sandboxes
