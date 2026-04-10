# Mobile Agent (Android / iOS) Design

**Date:** 2026-04-10
**Status:** Design — awaiting implementation plan

## Goal

Add first-class Android and iOS agent support to agentserver. Because mobile OS sandboxing prevents running a CLI tool like Claude Code directly, the mobile app itself implements a Claude Code-equivalent agent loop. The app is a single Flutter codebase that ships to both platforms.

The mobile agent is a peer to the existing `claudecode` and `opencode` agents — it registers in a workspace, appears in agent discovery, accepts delegated tasks, and can delegate tasks to other agents. Capabilities that cannot run on mobile (shell, Docker, full filesystem, LSP) are handled by discovering other agents in the workspace and delegating via the existing MCP bridge tools.

## Non-Goals

- Running arbitrary shell commands on the device (iOS/Android sandboxing makes this impossible)
- On-device container execution
- Full parity with Claude Code's `Bash` / `LSP` / `EnterWorktree` tools — these are delegated to remote agents
- Sharing the agent loop implementation between the mobile app and the existing Go CLI agent (different languages, different constraints)

## Architectural Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Flutter Mobile App                        │
│                                                               │
│  ┌──────────┐  ┌──────────────┐  ┌─────────────────────┐   │
│  │  Chat UI  │  │ Agent Loop   │  │   Tool Registry      │   │
│  │           │  │  Engine      │  │                      │   │
│  │ streaming │  │              │  │ Local tools          │   │
│  │ markdown  │  │ prompt →     │  │  Read / Write / Edit │   │
│  │ tool call │  │ Messages API │  │  Glob / Grep         │   │
│  │ visualize │  │ (streaming)  │  │  WebFetch/WebSearch  │   │
│  │           │  │ → parse tool │  │  AskUser / Notebook  │   │
│  └─────┬────┘  │   use blocks │  │                      │   │
│        │        │ → execute    │  │ Agent discovery tools│   │
│        │        │ → tool_result│  │  discover_agents     │   │
│        │        │ → repeat     │  │  delegate_task       │   │
│        │        └──────┬───────┘  │  check_task          │   │
│        │                │          │  send_message        │   │
│        │                │          │  read_inbox          │   │
│        │       ┌────────▼────┐    │                      │   │
│        │       │ LLM Client  │    │ MCP client           │   │
│        │       │ direct to   │    │  HTTP/SSE/WS         │   │
│        │       │ modelserver │    │  transports          │   │
│        │       └─────────────┘    │                      │   │
│        │                          │ Device tools         │   │
│        │                          │  camera / GPS /      │   │
│  ┌─────▼────┐  ┌───────────────┐  │  clipboard /         │   │
│  │  Session │  │ Connection    │  │  contacts / scanner /│   │
│  │  Manager │  │ Manager       │  │  notifications       │   │
│  │ (SQLite) │  │  WS tunnel +  │  └──────────────────────┘   │
│  └──────────┘  │  push notif   │                             │
│                └───────────────┘                             │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ Background service (Android FG / iOS BGTask)         │    │
│  └─────────────────────────────────────────────────────┘    │
└──────────┬───────────────────────────────┬──────────────────┘
           │ agent ops (HTTPS + WS)        │ LLM calls (HTTPS SSE)
           ▼                                ▼
┌─────────────────────────┐     ┌──────────────────────────┐
│   agentserver (:8080)    │     │   modelserver (external)  │
│                          │     │                           │
│ - auth / OIDC            │     │ - OAuth2 device flow      │
│ - agent register         │     │ - /v1/messages (direct)   │
│ - /api/tunnel/…          │     │                           │
│ - agent discovery        │     └──────────────────────────┘
│ - task delegation        │
│ - agent mailbox          │
│ - push dispatch (new)    │
│ - Hydra (device flow)    │
└────────┬─────────────────┘
         │ (cluster-internal only)
         ▼
┌─────────────────────────┐
│    llmproxy (:8081)      │  ONLY used by cloud sandboxes.
│                          │  Mobile and local PC agents
│                          │  do NOT call this.
└─────────────────────────┘
```

**Key architectural decisions:**

1. **On-device agent loop.** The mobile app implements the full Claude Code-style loop in Dart. It is a first-class agent, not a thin client to a server-side worker.
2. **Direct modelserver access.** LLM traffic goes directly from the device to modelserver using the device's own OAuth token. `llmproxy` remains cluster-internal and is used exclusively by cloud sandboxes.
3. **Agentserver for agent operations only.** Registration, discovery, task delegation, mailbox, and the WebSocket tunnel — same endpoints the CLI agent already uses.
4. **Mobile-specific capabilities as local tools.** Camera, GPS, clipboard, notifications, etc. are registered directly in the tool registry. Other agents access them by delegating tasks to the mobile agent (whose agent card advertises these skills).
5. **Delegation over execution.** Any tool that cannot run on the device (shell, Docker, LSP, heavy git ops) is delegated via `discover_agents` + `delegate_task` to other agents in the workspace.

## Agent Loop Engine

The heart of the app. A Dart implementation of Claude Code's agent loop.

### Loop flow

```
1. Build messages array:
     [system_prompt, ...conversation_history, new_user_message]

2. POST {modelserver}/v1/messages with Authorization: Bearer {modelserver_token}
     - stream: true
     - SSE response parsed incrementally
     - text deltas rendered into the chat UI in real time

3. When the stream completes, inspect the final message:
     - For each content block:
         - TextBlock → already displayed
         - ToolUseBlock → queue for execution

4. If no ToolUseBlocks → loop terminates, wait for next user turn.

5. Execute queued tool calls:
     - All tools with isReadOnly == true run concurrently (bounded, e.g. 10 at a time)
     - Any tool with isReadOnly == false forces the batch to go sequential
     - Each tool returns a ToolResult { is_error, content }

6. Append a user message containing tool_result blocks for every tool call.

7. If turn count < maxTurns → goto step 2.
   If turn count >= maxTurns → terminate with a warning message.
```

### Streaming

The app uses SSE parsing on the HTTPS response body. As `content_block_delta` events arrive, text is appended to the visible assistant message in the chat UI. Tool call blocks are rendered as collapsible cards that fill in as arguments stream.

### Context management

Conversation tokens are tracked per turn using the `usage` field returned by modelserver. When the cumulative token count approaches the model's context window:

1. Drop the oldest tool results first (they're usually the largest)
2. Then summarize older assistant / user pairs into a single synthetic message
3. Preserve the last N turns verbatim (N ~= 5)

### Max turns guard

Default cap of 50 turns per user prompt to prevent runaway loops. Configurable in settings.

### System prompt

Aligned with Claude Code's system prompt style. Includes:
- Role description ("You are a mobile agent running on Android/iOS…")
- Tool descriptions (auto-generated from registered tools)
- Guidelines for delegating to remote agents when a capability is unavailable locally
- The dynamically-refreshed agent listing formatted as markdown (same pattern as `internal/mcpbridge/listing.go`)

### Conversation persistence

After each turn, the full conversation is serialized to SQLite via `sqflite`. On app relaunch, the sessions list is restored and any session can be resumed at the exact point it stopped.

## Tool System

### Base tool interface

```dart
abstract class Tool {
  String get name;
  String get description;
  Map<String, dynamic> get inputSchema;  // JSON Schema for Claude API

  bool get isReadOnly;            // Affects batching
  bool get requiresConfirmation;  // Ask user before executing

  Future<ToolResult> call(Map<String, dynamic> args, ToolContext ctx);
}

class ToolResult {
  final bool isError;
  final List<ContentBlock> content;  // Text, image, etc.
}
```

Tools are registered in a `ToolRegistry`. The system prompt and the Messages API `tools` parameter are built from the registry at the start of each turn, so tools can be added or removed at runtime (e.g., when an MCP server connects or disconnects).

### Local tools (Claude Code parity)

| Tool | Claude Code equivalent | Notes |
|------|------------------------|-------|
| `Read` | FileReadTool | Reads files inside the app sandbox / selected workspace folder |
| `Write` | FileWriteTool | Writes files, creates parent dirs |
| `Edit` | FileEditTool | `old_string` / `new_string` replacement, same API as CC |
| `Glob` | GlobTool | `glob` package, patterns relative to workspace root |
| `Grep` | GrepTool | Pure-Dart regex search (no external ripgrep binary) |
| `WebFetch` | WebFetch | HTTPS GET, HTML → markdown, returns text |
| `WebSearch` | WebSearch | Uses a configurable search provider, similar to CC |
| `AskUser` | AskUserQuestion | Native Flutter dialog with option chips |
| `NotebookEdit` | NotebookEdit | JSON manipulation of `.ipynb` files |

### Agent discovery tools (MCP bridge parity)

These five tools replicate exactly what `internal/mcpbridge/tools.go` exposes to the Go CLI agent, but implemented natively in Dart and calling the same REST endpoints on agentserver:

| Tool | Endpoint | Purpose |
|------|----------|---------|
| `discover_agents` | `GET /api/agent/discovery/agents` | List agents in the workspace, filter by skill / tag / status |
| `delegate_task` | `POST /api/agent/tasks` | Delegate a task with prompt + optional skill, returns task_id |
| `check_task` | `GET /api/agent/tasks/{id}` | Poll task status and result |
| `send_message` | `POST /api/agent/mailbox/send` | Async message to another agent |
| `read_inbox` | `GET /api/agent/mailbox/inbox` | Read unread messages |

Like the Go MCP bridge, `delegate_task`'s description is **dynamically generated** on each turn. A background `AgentListing` refreshes the list of known agents from `/api/agent/discovery/agents` every 60 seconds; the formatted markdown listing is spliced into the tool description so the model always sees the current roster.

### MCP client tools (external servers)

The app embeds an MCP client that supports:
- **HTTP** transport (POST JSON-RPC)
- **SSE** transport (server-sent events for streaming)
- **WebSocket** transport

Stdio is not supported on mobile — the OS does not allow spawning subprocesses. Users configure MCP servers in app settings (URL + optional bearer token). Tools from each connected server are added to the registry with the `mcp__{server}__{tool}` naming convention, matching Claude Code.

### Mobile-specific tools (device capabilities)

Device capabilities are registered directly in the local tool registry as regular `Tool` implementations — no separate MCP server. This keeps the plumbing simple: the agent loop sees them alongside `Read` / `Write` / etc., and they follow the same confirmation and permission rules.

| Tool | Capability |
|------|------------|
| `capture_photo` | Take photo via camera |
| `pick_image` | Select image from gallery |
| `get_location` | GPS coordinates |
| `get_clipboard` / `set_clipboard` | Clipboard access |
| `show_notification` | Local notification |
| `read_contacts` | Contacts (with permission) |
| `scan_qrcode` | Camera-based QR / barcode scan |

**How other agents access these capabilities:** through task delegation. Another agent calls `delegate_task` with a prompt like "take a photo of the whiteboard and describe it" targeted at the mobile agent; the mobile agent's own loop executes `capture_photo` and returns the result. These tools are announced in the mobile agent's registered agent card (`display_name`, `description`, skills `[camera, location, clipboard, contacts, scanner]`, tags `[mobile]`) so they show up in `discover_agents` responses for the rest of the workspace.

### Tool execution flow

```
Messages API returns tool_use blocks
            │
            ▼
    Classify each tool
            │
            ├─ Local tool → run in Dart directly
            │
            ├─ Agent discovery tool → HTTP call to agentserver
            │
            └─ MCP tool → forward to connected MCP server
                          (may be remote or local)
            │
            ▼
    Build tool_result content block (possibly with is_error=true)
            │
            ▼
    Append to conversation messages, next turn
```

## Connection & Authentication

The mobile agent performs **two independent OAuth 2.0 Device Authorization Grant flows**, each against a different authorization server:

1. **Agentserver Hydra** for the `agent:register` scope. Used to register as an agent and call agentserver APIs (`/api/agent/discovery/*`, `/api/agent/tasks/*`, `/api/agent/mailbox/*`, tunnel).
2. **Modelserver** for the `project:inference` scope. Used to call modelserver's `/v1/messages` endpoint directly from the device.

### Flow 1 — Agentserver device flow

```
App Launch → Not logged in → Login Screen → User taps "Sign in to agentserver"
    │
    ▼
POST {agentserver}/api/oauth2/device/auth
  client_id=agentserver-agent-mobile
  scope=openid profile agent:register offline_access
    │
    ▼
Response: { device_code, user_code, verification_uri,
            verification_uri_complete, interval, expires_in }
    │
    ▼
App calls url_launcher.launchUrl(verification_uri_complete,
                                 mode: externalApplication)
  (opens system browser — NOT an in-app WebView, per RFC 8252)
    │
    │   App shows waiting screen with:
    │     - "Waiting for authorization..."
    │     - User code (fallback if the URL fails to preload it)
    │     - "[Open authorization page]" button (retry launchUrl)
    │     - "[Cancel]" button
    │
    │   Meanwhile polls POST {agentserver}/api/oauth2/token
    │     grant_type=urn:ietf:params:oauth:grant-type:device_code
    │     device_code={device_code}
    │     (respecting interval + slow_down per RFC 8628)
    │
    ▼
User approves in system browser → next poll returns 200 with tokens
    │
    ▼
POST {agentserver}/api/agent/register  Authorization: Bearer {access_token}
  Body: { name: "iPhone-xxx", type: "mobile" }
    │
    ▼
Response: { sandbox_id, tunnel_token, proxy_token, workspace_id, short_id }
    │
    ▼
Save all tokens to flutter_secure_storage, navigate to main chat screen.
```

**No QR code.** The device being authorized is the device the user is holding. The canonical "user code + QR" presentation from the CLI flow is unnecessary and confusing here. The user code is displayed as a plain-text fallback only.

### Flow 2 — Modelserver device flow

Same UX pattern, independent of Flow 1, typically triggered from "Settings → Connect LLM Provider" after the user has logged into agentserver:

```
POST {modelserver}/oauth/device/auth
  client_id=agentserver-mobile-client
  scope=project:inference offline_access
    │
    ▼
Response: { device_code, verification_uri_complete, … }
    │
    ▼
launchUrl(verification_uri_complete, mode: externalApplication)
    │
    ▼
Poll POST {modelserver}/oauth/token …
    │
    ▼
Save { modelserver_access_token, modelserver_refresh_token,
       modelserver_expires_at, modelserver_project_id }
```

### Token refresh

- Agentserver tokens refresh automatically when within 60 s of expiry using the standard `grant_type=refresh_token` call to `/api/oauth2/token`.
- Modelserver tokens refresh the same way against `{modelserver}/oauth/token`.
- On 401 from either endpoint, attempt one refresh; if that fails, force the user back to the login screen.

### Direct LLM calls

Every Messages API call from the mobile agent goes directly to modelserver:

```
POST {modelserver}/v1/messages
Authorization: Bearer {modelserver_access_token}
Accept: text/event-stream
Content-Type: application/json

{ "model": "…", "messages": [...], "tools": [...],
  "max_tokens": …, "stream": true }
```

Agentserver and llmproxy are **not** involved in this path.

### Connection manager

```
                   disconnected
                       ▲
                       │ (backoff 1s, 2s, 4s, … cap 60s)
                       │
              ┌────────┴────────┐
              │   connecting    │
              └────────┬────────┘
                       │
                       ▼
              ┌─────────────────┐
              │   connected     │
              │ (WS + heartbeat)│
              └─┬───────────────┘
                │
     app backgrounded │
                │
                ▼
     ┌────────────────────┐
     │  backgrounded       │
     │  WS closed          │
     │  listens for push   │
     └─┬─────────────────┘
       │  app foregrounded
       │
       ▼
    reconnect
```

**Foreground:** WebSocket tunnel to `/api/tunnel/mobile/{sandboxId}?token={tunnelToken}` (new mobile-optimized endpoint, see Server-side changes). Heartbeat every 30 s. Auto-reconnect with exponential backoff.

**Background:** WS closed. FCM / APNs push token registered with agentserver. When a task or message arrives, agentserver dispatches a push, the OS wakes the app, it reconnects and processes.

The agent loop itself does **not** depend on the WebSocket tunnel. Outbound Claude calls go directly to modelserver over plain HTTPS, so the loop keeps running even when the tunnel is down. The tunnel only matters for **incoming** requests from other agents (e.g., someone else calling mobile's MCP tools).

### Session persistence (SQLite)

```
sessions       (sandbox_id, workspace_id, server_url, agentserver_tokens_json,
                modelserver_tokens_json, push_token, created_at)
conversations  (id, sandbox_id, title, created_at, last_updated)
messages       (id, conversation_id, seq, role, content_blocks_json, usage_json,
                created_at)
mcp_servers    (id, url, transport, auth_token, name, enabled)
settings       (key, value)
```

Messages are stored in a normalized `messages` table (not as a JSON blob inside `conversations`) so individual turns can be queried, trimmed for context compression, or resumed without rewriting the full conversation on every turn.

## Background Execution

### Android — Foreground Service

Android permits foreground services with a persistent notification to run indefinitely. When the agent loop is active and the app is backgrounded:

- Start a `ForegroundService` with a notification "Agent running — processing task…"
- The agent loop continues normally
- The WS tunnel stays connected
- The notification updates with current turn number / current tool name
- When the loop idles or the user returns, stop the service and remove the notification

### iOS — BGProcessingTask (best effort)

iOS aggressively kills background tasks. Strategy:

1. **Short tasks (< 30 s):** `beginBackgroundTask(withName:)` — reliable.
2. **Medium tasks (< 30 min):** request a `BGProcessingTask`. Keep the WS alive via a background URLSession. The agent loop continues but may be killed at the OS's discretion.
3. **Long tasks:** If the loop is still running after ~10 minutes in the background, **auto-delegate** the remaining work: the agent calls `delegate_task` to hand the rest of the conversation off to a remote agent (e.g., a `claudecode` agent in the same workspace), then saves state and exits cleanly. When the remote agent completes, agentserver pushes a notification, and the user can return to the app to see the result.

### State preservation

At every turn boundary, the agent loop checkpoints:
- Full conversation messages
- Current turn number and token usage
- Pending tool results (if any)

If the process is killed mid-turn, resume shows the last committed assistant message; the user can re-prompt from there.

### Notifications

```
Push type            | UI                                      | Tap action
---------------------|-----------------------------------------|----------------
task_assigned        | "New task from Agent-X"                 | Open the task view
message_received     | "Message from Agent-X: <first line>"    | Open inbox
agent_loop_completed | "Task finished: <summary>"              | Open conversation
agent_loop_paused    | "Agent paused (iOS background limit)"   | Resume agent loop
```

## Server-Side Changes

All server-side changes are in agentserver. `llmproxy` is untouched. `modelserver` has an external dependency (see below).

### 1. Hydra OAuth client for mobile

Add a new public OAuth client `agentserver-agent-mobile` (peer of the existing `agentserver-agent-cli`). Separate client ID allows independent tracking, revocation, and analytics.

```yaml
- client_id: agentserver-agent-mobile
  client_name: Agentserver Mobile Agent
  grant_types:
    - urn:ietf:params:oauth:grant-type:device_code
    - refresh_token
  scope: "openid profile agent:register offline_access"
  token_endpoint_auth_method: none    # public client
```

### 2. Push notification registration

New endpoint:

```
POST /api/agent/push/register
Auth: Bearer {proxy_token}
Body: { platform: "fcm" | "apns", token: "…", app_version: "…" }
```

Stores the push token in a new table keyed by `sandbox_id`. Used by the server to dispatch push notifications when the agent isn't currently connected via WebSocket.

### 3. Push dispatcher service

New package `internal/push/`:

```
internal/push/
├── dispatcher.go    # Unified Send() interface
├── fcm.go           # Firebase Cloud Messaging
├── apns.go          # Apple Push Notification service
└── config.go
```

Hooks added to:
- `internal/server/agent_tasks.go` — on task creation, check if target is a mobile agent and not connected; if so dispatch push
- `internal/server/agent_mailbox.go` — same on message send

### 4. Mobile-optimized tunnel endpoint

```
WS /api/tunnel/mobile/{sandboxId}?token={tunnelToken}
```

Same authorization model as `/api/tunnel/{sandboxId}` but uses **plain JSON message framing** instead of yamux. Rationale: a pure-Dart yamux implementation adds significant complexity to the Flutter app for little benefit; mobile doesn't need terminal streams, only HTTP-style requests and agent info updates.

Message types (all JSON objects, newline-delimited or length-framed):

```
{ "type": "http_request",  "id": "…", "method": "…", "path": "…",
  "headers": {…}, "body_b64": "…" }
{ "type": "http_response", "id": "…", "status": 200,
  "headers": {…}, "body_b64": "…" }
{ "type": "agent_info", "status": "idle" | "busy", "current_task": "…" }
{ "type": "ping" }
{ "type": "pong" }
```

The existing `/api/tunnel/{sandboxId}` endpoint stays unchanged for CLI agents.

### 5. Database migration

```sql
-- internal/db/migrations/0XX_agent_push_tokens.sql
CREATE TABLE agent_push_tokens (
    sandbox_id  TEXT PRIMARY KEY REFERENCES sandboxes(id) ON DELETE CASCADE,
    platform    TEXT NOT NULL CHECK (platform IN ('fcm','apns')),
    token       TEXT NOT NULL,
    app_version TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_push_tokens_platform ON agent_push_tokens(platform);
```

### 6. New server config env vars

```
PUSH_FCM_CREDENTIALS_PATH=/etc/agentserver/fcm.json
PUSH_APNS_KEY_PATH=/etc/agentserver/apns.p8
PUSH_APNS_KEY_ID=…
PUSH_APNS_TEAM_ID=…
PUSH_APNS_BUNDLE_ID=com.example.agentserver.mobile
```

### Endpoints reused as-is (no changes)

- `POST /api/auth/login`, `POST /api/auth/register`
- `GET /api/auth/oidc/providers`, `GET /api/auth/oidc/{provider}/login`
- `POST /api/oauth2/device/auth`, `POST /api/oauth2/token`
- `POST /api/agent/register`
- `POST /api/agent/discovery/cards`, `GET /api/agent/discovery/agents`
- `POST /api/agent/tasks`, `GET /api/agent/tasks/{id}`
- `POST /api/agent/mailbox/send`, `GET /api/agent/mailbox/inbox`

### External dependency: modelserver device flow

Modelserver must support:
- OAuth 2.0 Device Authorization Grant (RFC 8628) at `{modelserver}/oauth/device/auth`
- Token endpoint accepting `device_code` grant
- Refresh token support
- A public client (e.g., `agentserver-mobile-client`) with scopes `project:inference offline_access`
- Acceptance of end-user bearer tokens on `/v1/messages` (not just the cluster-internal workspace tokens)

If modelserver does not yet support device flow, coordinating that change is a prerequisite and must be captured as a separate task in the implementation plan.

## Project Structure

### Flutter app layout

```
mobile-agent/
├── pubspec.yaml
├── README.md
├── analysis_options.yaml
├── android/
│   └── app/src/main/
│       ├── AndroidManifest.xml          # FG service, permissions
│       └── kotlin/.../ForegroundService.kt
├── ios/
│   └── Runner/
│       ├── Info.plist                    # BGTaskSchedulerPermittedIdentifiers
│       └── AppDelegate.swift             # Push notification handling
├── lib/
│   ├── main.dart
│   ├── app.dart                          # MaterialApp, routing
│   │
│   ├── auth/
│   │   ├── device_flow_client.dart       # RFC 8628 client
│   │   ├── agentserver_auth.dart
│   │   ├── modelserver_auth.dart
│   │   ├── login_screen.dart
│   │   └── session.dart
│   │
│   ├── api/                              # agentserver REST client
│   │   ├── agentserver_client.dart
│   │   ├── agent_register.dart
│   │   ├── agent_discovery.dart
│   │   ├── agent_tasks.dart
│   │   ├── agent_mailbox.dart
│   │   └── push_register.dart
│   │
│   ├── llm/                              # modelserver / Messages API client
│   │   ├── modelserver_client.dart       # POST /v1/messages, streaming
│   │   ├── message_types.dart
│   │   └── sse_parser.dart
│   │
│   ├── agent/                            # Agent loop engine
│   │   ├── agent_loop.dart
│   │   ├── conversation.dart
│   │   ├── system_prompt.dart
│   │   └── context_manager.dart
│   │
│   ├── tools/
│   │   ├── tool.dart
│   │   ├── tool_registry.dart
│   │   ├── tool_executor.dart
│   │   │
│   │   ├── local/
│   │   │   ├── read_tool.dart
│   │   │   ├── write_tool.dart
│   │   │   ├── edit_tool.dart
│   │   │   ├── glob_tool.dart
│   │   │   ├── grep_tool.dart
│   │   │   ├── web_fetch_tool.dart
│   │   │   ├── web_search_tool.dart
│   │   │   ├── ask_user_tool.dart
│   │   │   └── notebook_edit_tool.dart
│   │   │
│   │   ├── agent/
│   │   │   ├── discover_agents_tool.dart
│   │   │   ├── delegate_task_tool.dart
│   │   │   ├── check_task_tool.dart
│   │   │   ├── send_message_tool.dart
│   │   │   ├── read_inbox_tool.dart
│   │   │   └── agent_listing.dart        # 60s refresh
│   │   │
│   │   ├── mcp/
│   │   │   ├── mcp_client.dart           # HTTP / SSE / WS transports
│   │   │   ├── mcp_tool_adapter.dart
│   │   │   └── mcp_config.dart
│   │   │
│   │   └── device/                       # Device capability tools
│   │       ├── capture_photo.dart
│   │       ├── pick_image.dart
│   │       ├── get_location.dart
│   │       ├── clipboard.dart
│   │       ├── notification.dart
│   │       ├── contacts.dart
│   │       └── scan_qrcode.dart
│   │
│   ├── connection/
│   │   ├── connection_manager.dart
│   │   ├── tunnel_client.dart            # JSON-framed WS
│   │   ├── push_handler.dart             # FCM / APNs
│   │   └── lifecycle.dart                # FG / BG
│   │
│   ├── background/
│   │   ├── background_service.dart
│   │   └── checkpoint.dart
│   │
│   ├── storage/
│   │   ├── database.dart                 # sqflite setup
│   │   ├── session_repo.dart
│   │   ├── conversation_repo.dart
│   │   └── settings_repo.dart
│   │
│   └── ui/
│       ├── chat/
│       │   ├── chat_screen.dart
│       │   ├── message_bubble.dart
│       │   ├── tool_call_view.dart
│       │   ├── streaming_text.dart
│       │   └── input_box.dart
│       ├── sessions/
│       │   └── session_list.dart
│       ├── agents/
│       │   └── agent_list_screen.dart
│       ├── settings/
│       │   ├── settings_screen.dart
│       │   ├── mcp_servers_screen.dart
│       │   └── server_config_screen.dart
│       └── widgets/
│           └── markdown_view.dart
│
└── test/
    ├── agent/
    │   └── agent_loop_test.dart
    ├── tools/
    │   ├── read_tool_test.dart
    │   ├── edit_tool_test.dart
    │   └── discover_agents_test.dart
    └── integration/
        └── end_to_end_test.dart
```

### Server-side layout

```
internal/
├── push/                                 # NEW
│   ├── dispatcher.go
│   ├── fcm.go
│   ├── apns.go
│   └── config.go
├── server/
│   ├── agent_push.go                     # NEW — /api/agent/push/register
│   ├── agent_tasks.go                    # MODIFIED — push dispatch on create
│   ├── agent_mailbox.go                  # MODIFIED — push dispatch on send
│   └── server.go                         # MODIFIED — wire new routes
├── tunnel/
│   └── mobile_tunnel.go                  # NEW — JSON-framed WS handler
└── db/
    ├── migrations/
    │   └── 0XX_agent_push_tokens.sql     # NEW
    └── push_tokens.go                    # NEW — CRUD helpers
```

### Key Dart dependencies

```
http / dio            REST client
web_socket_channel    WebSocket
sqflite               Local SQLite
flutter_secure_storage Token storage
firebase_messaging    FCM push (Android + iOS)
flutter_local_notifications Local notifications
flutter_background_service Android foreground service
image_picker / camera Camera access
geolocator            GPS
glob                  Glob pattern matching
url_launcher          Open system browser for OAuth
flutter_markdown      Chat rendering
json_schema           Tool input validation
uuid                  ID generation
```

## Error Handling

### LLM API errors

```
modelserver error response
    │
    ├─ 401 → attempt refresh; if refresh fails, force re-login
    ├─ 429 → exponential backoff, retry up to 3 times
    ├─ 500/503 → retry with backoff
    ├─ 400 → surface in chat as error (likely malformed tool result)
    └─ network timeout → one retry, then error in chat
```

### Tool execution errors

Every tool call returns a `ToolResult`. Exceptions are caught and wrapped as:

```
ToolResult { isError: true, content: [TextBlock("<error message>")] }
```

The error result is fed back to the model as a `tool_result` block with `is_error: true`. The model decides how to recover (retry with different args, try a different approach, give up and explain). Tool errors are **non-fatal** to the agent loop.

### Delegation timeouts

- `delegate_task` creates a task with a default 10-minute timeout (configurable per call)
- The mobile agent can poll via `check_task` while waiting, or wait asynchronously and return to the user as soon as it completes
- User can cancel from chat UI → `POST /api/tasks/{id}/cancel`
- On timeout, the task is marked failed server-side and the next `check_task` returns the failure

### Connection loss

- Tunnel WS disconnects → exponential backoff reconnect (if in foreground)
- Agent loop is unaffected (outbound calls go direct to modelserver over plain HTTPS)
- In background → switch to push mode, reconnect when a push arrives

### State corruption / app crash

- Conversation state is persisted to SQLite after every turn
- On app relaunch, sessions list is restored
- If the crash happened mid-turn, the last tool call's result is lost — resume shows the last committed assistant message and the user can re-prompt

### MCP server disconnect

- Tools from the disconnected server are removed from the registry
- The system prompt rebuilt on the next turn will not include them
- Reconnect attempted every 30 s
- Small banner in the chat UI notifies the user

### Permission denials (camera, GPS, contacts)

- First use of a capability prompts the OS permission dialog
- If denied, the corresponding MCP tool returns `ToolResult { isError: true, content: [...] }`
- Settings screen has a toggle to re-request

## Testing Strategy

### Unit tests

**Agent loop** with a mocked modelserver client:
- Text-only response → loop exits after one turn
- Single tool call → execute → feed result back → continue
- Multiple concurrent read-only tool calls → batched
- Tool error → wrapped as `is_error` tool_result, loop continues
- Max turns reached → loop terminates gracefully
- Token budget exceeded → context compression

**Each tool implementation:**
- `Read` / `Write` / `Edit` against temp directories
- `Glob` / `Grep` pattern matching correctness
- `discover_agents` / `delegate_task` / `check_task` against a mocked HTTP server
- MCP client handling successful and error JSON-RPC responses

**Conversation persistence:** SQLite round-trips.

**Tool input validation:** JSON Schema validation rejects malformed inputs before execution.

**Auth flows:** mocked HTTP servers for both agentserver and modelserver device flow endpoints, including error cases (`authorization_pending`, `slow_down`, `access_denied`, `expired_token`).

### Integration tests

**End-to-end against a test agentserver:**
- Spin up Go agentserver with in-memory DB in test mode
- Mobile agent logs in, registers, sends a prompt
- Verify conversation is persisted, tool calls are made, tasks can be delegated

**Cross-agent delegation:**
- Start a fake CLI agent that accepts tasks and returns canned results
- Mobile agent receives a prompt "run ls"
- Mobile agent delegates to CLI agent, waits, shows the result in chat

### Manual testing checklist

- [ ] Login via agentserver device flow (system browser)
- [ ] Login via modelserver device flow (system browser)
- [ ] Send chat message, receive streaming response
- [ ] Tool call visualization (Read, Edit, Grep) in chat
- [ ] Delegate task to a CLI agent, see result in chat
- [ ] Background app during active task (Android)
- [ ] Background app during active task (iOS)
- [ ] Receive push notification, tap to resume
- [ ] Camera tool invoked by another agent in workspace
- [ ] MCP server connection (external HTTP / SSE)
- [ ] Session resume after app kill
- [ ] Airplane mode → reconnect → continue

### Device coverage

- Android API 26+ (Android 8.0+)
- iOS 14+
- Both a mid-range and flagship device per platform

## Open Questions / Risks

1. **Modelserver device flow support.** The spec assumes modelserver supports OAuth 2.0 device grant. If not, this is a prerequisite that must be completed before the mobile client can ship.
2. **iOS background execution limits.** The "auto-delegate long tasks to remote agents" strategy is a fallback but has edge cases (e.g., what if no capable remote agent exists in the workspace?). The implementation plan should specify the user-visible behavior for this case.
3. **Tunnel protocol divergence.** Adding a JSON-framed mobile tunnel endpoint means the server has two tunnel implementations to maintain. Acceptable because the logic is small, but worth noting.
4. **File access scope.** Mobile file tools operate on app sandbox + user-selected workspace folders. The scope needs to be clearly communicated in the UI so the user understands what "Read /path" means on mobile.
5. **Push notification costs.** FCM is free. APNs requires an Apple Developer account and certificate. Operationally the deployment needs both.
