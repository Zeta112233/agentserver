# Developer Platform: Custom Agent SDK & Protocol

**Date:** 2026-04-15
**Status:** Draft

## Overview

Enable developers to build custom agents in any programming language and connect them to agentserver through a standard protocol. This extends the existing yamux tunnel infrastructure (proven with opencode/claudecode agents) to support a new `custom` agent type, with comprehensive protocol documentation and a Go reference implementation.

## Goals

- Developers can register and connect custom agents using any language that supports WebSocket + yamux
- Custom agents support two modes: HTTP service (Web UI proxy) and task executor (worker)
- Protocol-first approach: detailed protocol documentation with reference implementations, not opinionated SDKs
- Minimal server-side changes: extend existing infrastructure rather than building new systems

## Non-Goals

- Agent marketplace or distribution system
- Custom sandbox environments (Docker images, custom runtimes)
- Credential proxy access for custom agents (future work)
- IM bridge integration for custom agents (future work)
- WebSocket upgrade proxying through the tunnel

---

## 1. Sandbox Type Extension

### New Type: `custom`

Add `custom` as a valid sandbox type alongside `opencode` and `claudecode`.

**Registration API** (extend existing `POST /api/agent/register`):

```
POST /api/agent/register
Authorization: Bearer {oauth_access_token}
Content-Type: application/json

{
  "name": "My Custom Agent",
  "type": "custom"
}
```

**Response (201 Created):**

```json
{
  "sandbox_id": "uuid-v4",
  "tunnel_token": "32-char-hex",
  "proxy_token": "32-char-hex",
  "short_id": "16-char-shortid",
  "workspace_id": "uuid-v4"
}
```

### Differences from opencode/claudecode

| Behavior | opencode | claudecode | custom |
|----------|----------|------------|--------|
| `opencode_password` generated | Yes | No | No |
| `Authorization: Basic` injected by proxy | Yes | No | No |
| Tunnel protocol | yamux | yamux | yamux |
| Task polling | Yes | Yes | Yes |
| HTTP proxy through tunnel | Yes | Yes | Yes |
| Terminal streams | Yes | Yes | Optional (agent may reject) |

### Server-Side Changes

1. **`internal/server/agent_register.go`**: Expand type validation to accept `"custom"`:
   ```go
   // Before:
   if sandboxType != "opencode" && sandboxType != "claudecode" {
   // After:
   if sandboxType != "opencode" && sandboxType != "claudecode" && sandboxType != "custom" {
   ```
   For `custom` type: skip `opencode_password` generation (same as `claudecode`).

2. **`internal/sandboxproxy/tunnel.go:proxyViaTunnel`**: Skip Basic Auth injection for `custom` type:
   ```go
   // Only inject auth for opencode sandboxes
   if sbx.Type == "opencode" && sbx.OpencodeToken != "" {
       headers["Authorization"] = "Basic " + base64(...)
   }
   ```

3. **Database**: No schema changes needed. `sandboxes.type` is already a `text` field.

---

## 2. Connection Protocol

### Authentication Flow

Developers authenticate via the existing OAuth Device Flow:

```
Developer Code                    AgentServer                    Browser
     |                                |                            |
     |-- POST /api/oauth2/device/auth |                            |
     |   client_id=agentserver-agent-cli                           |
     |   scope=openid profile agent:register                      |
     |<-- { device_code, user_code,   |                            |
     |      verification_uri_complete }|                            |
     |                                |                            |
     |   (display URL/QR to user)     |                            |
     |                                |<-- user visits URL --------|
     |                                |    authenticates + consents |
     |                                |-- redirect back ---------->|
     |                                |                            |
     |-- POST /api/oauth2/token       |                            |
     |   grant_type=device_code       |                            |
     |   device_code=...              |                            |
     |<-- { access_token }            |                            |
     |                                |                            |
     |-- POST /api/agent/register     |                            |
     |   Authorization: Bearer {token}|                            |
     |   { name, type: "custom" }     |                            |
     |<-- { sandbox_id, tunnel_token, |                            |
     |      proxy_token, short_id }   |                            |
```

### Tunnel Connection

After registration, the agent establishes a persistent WebSocket + yamux tunnel:

1. **WebSocket**: Connect to `wss://{server}/api/tunnel/{sandbox_id}?token={tunnel_token}`
2. **yamux**: Create a yamux client session over the WebSocket connection
3. **Heartbeat**: Send control streams every 20 seconds
4. **Accept streams**: Handle server-initiated HTTP and terminal streams

### yamux Configuration

Agents should use these yamux settings to match the server:

| Setting | Value |
|---------|-------|
| EnableKeepAlive | false |
| ConnectionWriteTimeout | 10s |
| AcceptBacklog | 256 |

### Stream Protocol

Every yamux stream begins with a header:

```
┌─────────────┬──────────────────────┬─────────────────────────────┐
│ 1 byte      │ 4 bytes (big-endian) │ variable length             │
│ stream_type │ metadata_length      │ metadata (JSON)             │
└─────────────┴──────────────────────┴─────────────────────────────┘
```

### Stream Types

| Type | Value | Direction | Purpose |
|------|-------|-----------|---------|
| HTTP | `0x01` | Server → Agent | Proxy HTTP request to agent |
| Terminal | `0x02` | Server → Agent | Bidirectional terminal I/O |
| Control | `0x03` | Agent → Server | Heartbeat + agent info |

### HTTP Stream (0x01) — Required

**Server sends to agent:**

```
[stream header: type=0x01, metadata=HTTPStreamMeta]
[request body: exactly body_len bytes]
```

```json
// HTTPStreamMeta
{
  "method": "GET",
  "path": "/api/status",
  "headers": {
    "Accept": "application/json",
    "Cookie": "session=abc123"
  },
  "body_len": 0
}
```

**Agent responds:**

```
[stream header: type=0x01, metadata=HTTPResponseMeta]
[response body: write until stream close]
```

The response uses the same stream header format (1 byte type + 4 bytes metadata length + metadata JSON). The server calls `ReadStreamHeader()` to parse it.

```json
// HTTPResponseMeta
{
  "status": 200,
  "headers": {
    "Content-Type": "application/json"
  }
}
```

**Streaming responses**: Agent can write response body incrementally. The server flushes chunks to the end user as they arrive (supports SSE, chunked transfer).

### Terminal Stream (0x02) — Optional

For custom agents, terminal streams are optional. The agent may close the stream immediately upon receiving it. If implemented:

- No metadata (metadata_length = 0)
- Bidirectional raw bytes after header
- Typically used for interactive shell sessions

### Control Stream (0x03) — Required

Agent-initiated, sent every 20 seconds:

```
[stream header: type=0x03, metadata=AgentInfo]
```

```json
// AgentInfo — minimal required fields
{
  "hostname": "dev-laptop",
  "os": "linux"
}
```

```json
// AgentInfo — full (optional fields)
{
  "hostname": "dev-laptop",
  "os": "linux",
  "platform": "ubuntu",
  "platform_version": "22.04",
  "kernel_arch": "x86_64",
  "cpu_model_name": "AMD Ryzen 9",
  "cpu_count_logical": 16,
  "memory_total": 34359738368,
  "disk_total": 1000204886016,
  "disk_free": 500102443008,
  "agent_version": "1.0.0",
  "workdir": "/home/dev/project",
  "capabilities": {
    "languages": [{"name": "python3", "version": "3.11.0", "path": "/usr/bin/python3"}],
    "tools": [{"name": "docker", "version": "24.0.0", "path": "/usr/bin/docker"}]
  }
}
```

The server uses this to:
- Update `last_heartbeat_at` (liveness tracking)
- Store agent metadata in `agent_info` table
- Build agent discovery card from capabilities

---

## 3. HTTP Proxy & Web UI

### How It Works

When a custom agent provides a Web UI, external users access it through the sandboxproxy subdomain:

```
User Browser                  SandboxProxy                    Agent (local)
     |                            |                               |
     |-- GET code-{shortID}.      |                               |
     |   example.com/dashboard    |                               |
     |                            |                               |
     |                            |-- lookup sandbox by shortID   |
     |                            |   check is_local = true       |
     |                            |   get tunnel from registry    |
     |                            |                               |
     |                            |-- open yamux stream --------->|
     |                            |   [HTTP meta + body]          |
     |                            |                               |
     |                            |<-- [response meta + body] ---|
     |                            |                               |
     |<-- 200 OK + response body  |                               |
```

### Routing Rules

- Subdomain pattern: `code-{shortID}.{baseDomain}` (reuses existing `OpencodeSubdomainPrefix`)
- SandboxProxy identifies the sandbox by `shortID`, checks `is_local`, then proxies via tunnel
- For `custom` type: no `Authorization: Basic` header injected (unlike `opencode`)
- Request timeout: 120 seconds (matching existing behavior)

### Streaming Support

- The sandboxproxy uses `http.Flusher` to forward chunks as they arrive
- Custom agents can serve Server-Sent Events (SSE), chunked responses, and long-polling endpoints
- WebSocket upgrade is NOT supported through the tunnel (yamux streams are unidirectional HTTP proxies)

### Agent-Side Implementation

The agent should:
1. Accept `StreamTypeHTTP` (0x01) streams from the yamux session
2. Parse `HTTPStreamMeta` to reconstruct the incoming HTTP request
3. Forward the request to a local HTTP server (the agent's Web UI)
4. Write `HTTPResponseMeta` + response body back to the stream
5. Close the stream when done

---

## 4. Task Executor Mode

### Overview

Custom agents can act as task workers, polling for assigned tasks via HTTP API. This reuses the existing agent task system with no protocol changes.

### Task Polling

```
GET /api/agent/tasks/poll
Authorization: Bearer {proxy_token}

Response 200 (task available):
{
  "task_id": "task_abc123",
  "skill": "code_review",
  "prompt": "Review this PR for security issues",
  "system_context": "You are a security reviewer",
  "timeout_seconds": 300,
  "created_at": "2026-04-15T10:00:00Z"
}

Response 204 (no tasks):
(empty body)
```

The server atomically marks the task as `assigned` when returned, preventing double-pickup.

### Task Status Updates

```
PUT /api/agent/tasks/{task_id}/status
Authorization: Bearer {proxy_token}
Content-Type: application/json

{
  "status": "completed",
  "result": {
    "output": "Found 2 potential SQL injection vulnerabilities..."
  },
  "cost_usd": 0.0,
  "num_turns": 1
}
```

Valid status transitions: `assigned → running → completed|failed`

### Task Creation (by other agents or Web UI)

```
POST /api/workspaces/{wid}/tasks
Authorization: Cookie or Bearer
Content-Type: application/json

{
  "target_id": "{custom_agent_sandbox_id}",
  "skill": "code_review",
  "prompt": "Review this PR",
  "timeout_seconds": 300
}
```

### Recommended Poll Interval

- 5 seconds when idle
- Immediate re-poll after completing a task
- Backoff to 30 seconds after extended idle period (no tasks for 5 minutes)

---

## 5. Agent Discovery & Capability Declaration

### Register Agent Card

Custom agents declare their capabilities via the existing agent discovery API:

```
POST /api/agent/discovery/cards
Authorization: Bearer {proxy_token}
Content-Type: application/json

{
  "display_name": "Code Review Bot",
  "description": "Automated code review powered by custom LLM pipeline",
  "agent_type": "custom",
  "card": {
    "skills": ["code_review", "lint", "security_scan"],
    "accepts_tasks": true,
    "has_web_ui": true,
    "version": "1.0.0"
  }
}
```

### Query Agents

```
GET /api/workspaces/{wid}/agents

Response:
[
  {
    "agent_id": "sandbox-uuid",
    "display_name": "Code Review Bot",
    "description": "Automated code review...",
    "agent_type": "custom",
    "status": "available",
    "card": { ... }
  }
]
```

### Status Management

- Heartbeat received within last 60 seconds → `status: "available"`
- Heartbeat stale > 60 seconds → `status: "offline"`
- This uses the existing idle watcher logic

### Web UI Display

- Custom agents appear in the workspace sandbox list alongside opencode/claudecode sandboxes
- If `has_web_ui: true` in the agent card, the UI provides a link to `code-{shortID}.{baseDomain}`
- If `accepts_tasks: true`, the agent appears as a valid task delegation target

---

## 6. Go Reference Implementation (agentsdk)

### Package Structure

Extract common agent logic from `internal/agent/` into a public package:

```
pkg/agentsdk/
├── client.go       // AgentClient: registration, tunnel, heartbeat
├── login.go        // OAuth Device Flow helper
├── handlers.go     // Handler interfaces for HTTP/Task/Terminal
├── config.go       // Configuration
└── doc.go          // Package documentation
```

### Developer API

```go
package main

import "github.com/user/agentserver/pkg/agentsdk"

func main() {
    client := agentsdk.NewClient(agentsdk.Config{
        ServerURL: "https://agent.example.com",
        Name:      "My Review Bot",
        Type:      "custom",  // defaults to "custom"
    })

    // Step 1: Login via Device Flow
    // Opens browser for user to authenticate
    token, err := client.Login(ctx)

    // Step 2: Register agent
    reg, err := client.Register(ctx, token)
    // reg contains: SandboxID, TunnelToken, ProxyToken, ShortID

    // Step 3: Connect and serve
    err = client.Connect(ctx, agentsdk.Handlers{
        // Handle proxied HTTP requests (optional)
        HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Serve your Web UI here
            w.Write([]byte("Hello from custom agent!"))
        }),

        // Handle assigned tasks (optional)
        Task: func(ctx context.Context, task agentsdk.Task) error {
            // Execute task logic
            return task.Complete(ctx, agentsdk.TaskResult{
                Output: "Review completed: no issues found",
            })
        },
    })
}
```

### Implementation Details

- `Login()`: Wraps `internal/agent/login.go` Device Flow logic
- `Register()`: Calls `POST /api/agent/register` with Bearer token
- `Connect()`: Establishes WebSocket + yamux, runs heartbeat loop, dispatches streams to handlers
- `Handlers.HTTP`: Adapts yamux HTTP streams to standard `http.Handler` — reconstructs `*http.Request`, captures `http.ResponseWriter` output, writes back to stream
- `Handlers.Task`: Poll loop wrapper — polls `GET /api/agent/tasks/poll`, calls handler, updates status
- Auto-reconnection with exponential backoff (1s → 60s), matching existing `internal/agent/client.go` behavior

### What Gets Extracted vs. What Stays

| Component | `pkg/agentsdk/` (public) | `internal/agent/` (internal) |
|-----------|-------------------------|------------------------------|
| Login (Device Flow) | Yes | Delegates to agentsdk |
| Registration | Yes | Delegates to agentsdk |
| Tunnel connection | Yes | Delegates to agentsdk |
| Heartbeat loop | Yes | Delegates to agentsdk |
| HTTP stream adapter | Yes (generic) | Uses agentsdk |
| Claude binary management | No | Stays internal |
| Opencode subprocess | No | Stays internal |
| MCP bridge | No | Stays internal |

---

## 7. Protocol Documentation

### Document Structure

```
docs/developer/
├── protocol.md           # Complete protocol specification
├── quickstart.md         # 5-minute getting started guide
├── api-reference.md      # HTTP API reference for agent endpoints
└── examples/
    ├── go/
    │   └── main.go       # Complete Go example using agentsdk
    ├── python/
    │   └── agent.py      # Python example with yamux
    └── node/
        └── agent.js      # Node.js example
```

### protocol.md Contents

1. **Overview**: What is a custom agent, what can it do
2. **Authentication**: OAuth Device Flow step-by-step
3. **Registration**: POST /api/agent/register request/response
4. **Tunnel Protocol**: WebSocket connection, yamux setup, configuration
5. **Stream Types**: Binary format, metadata schemas, lifecycle
6. **HTTP Proxy Protocol**: Full request/response cycle with examples
7. **Heartbeat Protocol**: Control stream format, timing requirements
8. **Task Execution**: Poll API, status updates, timeout handling
9. **Agent Discovery**: Capability registration, status management
10. **Error Handling**: Reconnection strategy, error codes, timeouts

### quickstart.md Contents

Minimal working example:
1. Install `agentsdk` (Go) or reference yamux library
2. Run login → register → connect in 20 lines of code
3. Serve a "Hello World" Web UI through the tunnel
4. Verify it works via `code-{shortID}.{baseDomain}`

### Language-Specific Examples

**Python example**: Uses `websockets` library + custom yamux implementation (simplified, single-stream) or `python-yamux` package. Shows Device Flow login, registration, tunnel connection, heartbeat, and HTTP request handling.

**Node.js example**: Uses `ws` library + `@parity/yamux`. Same coverage as Python.

Each example is a single self-contained file that developers can copy and modify.

---

## 8. Implementation Scope Summary

### Server-Side Changes (Minimal)

| File | Change |
|------|--------|
| `internal/server/agent_register.go` | Accept `type: "custom"`, skip password generation |
| `internal/sandboxproxy/tunnel.go` | Skip Basic Auth for `custom` type |
| Web UI (WorkspaceDetail.tsx) | Show custom agents with Web UI link |

### New Code

| Path | Description |
|------|-------------|
| `pkg/agentsdk/` | Public Go SDK package |
| `docs/developer/protocol.md` | Protocol specification |
| `docs/developer/quickstart.md` | Getting started guide |
| `docs/developer/api-reference.md` | API reference |
| `docs/developer/examples/` | Multi-language examples |

### Existing Code to Refactor

| From | To | Purpose |
|------|----|---------|
| `internal/agent/login.go` | `pkg/agentsdk/login.go` | Device Flow logic |
| `internal/agent/client.go` (partial) | `pkg/agentsdk/client.go` | Tunnel + heartbeat logic |

`internal/agent/` continues to exist but delegates to `pkg/agentsdk/` for shared functionality (login, tunnel, heartbeat). Agent-specific logic (Claude binary management, opencode subprocess, MCP bridge) stays in `internal/agent/`.

---

## 9. Security Considerations

- **Authentication**: Custom agents use the same OAuth + token model as existing agents. No new auth vectors.
- **Authorization**: Workspace membership check on registration (developer role or above). No approval required.
- **Tunnel isolation**: Each agent gets its own sandbox ID, tunnel token, and proxy token. Tokens are 128-bit random.
- **No credential proxy access**: Custom agents cannot access workspace credentials in this phase. This limits blast radius.
- **HTTP proxy**: Requests to custom agents go through the same sandboxproxy path as existing agents. No direct network access.
- **Rate limiting**: Custom agents are subject to existing workspace quotas for sandbox count.

---

## 10. Future Work (Out of Scope)

- **Credential proxy access**: Allow custom agents to access workspace credentials (GitHub tokens, etc.)
- **IM bridge integration**: Let custom agents send/receive messages via WeChat/Slack
- **Agent-to-agent delegation**: Custom agents delegating tasks to other agents
- **Custom Docker images**: Developers defining custom runtime environments
- **Agent marketplace**: Publishing and discovering agents across workspaces
- **WebSocket proxy**: Supporting WebSocket upgrade through the tunnel
