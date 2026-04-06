# Mobile Agent Integration Guide

Connect your iOS/Android agent to agentserver using the CCR V2-compatible bridge protocol.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  agentserver (cloud)                                        │
│                                                             │
│  Registration:  POST /api/agent/register                    │
│  Card:          POST /api/agent/discovery/cards              │
│  Task Poll:     GET  /api/agent/tasks/poll                  │
│  Task Status:   PUT  /api/agent/tasks/{id}/status           │
│                                                             │
│  Bridge Session (CCR V2):                                   │
│    POST /v1/agent/sessions              → session_id        │
│    POST /v1/agent/sessions/{id}/bridge  → worker_jwt        │
│    GET  .../worker/events/stream        ← SSE (read)        │
│    POST .../worker/events               → HTTP POST (write) │
│    POST .../worker/heartbeat            → 20s keep-alive    │
│    PUT  .../worker                      → state reporting   │
└─────────────────────────────────────────────────────────────┘
           ▲ SSE (read)     │ HTTP POST (write)
           │                ▼
┌─────────────────────────────────────────┐
│  Mobile Agent (iOS / Android)           │
│                                         │
│  1. Register once (get tunnel_token)    │
│  2. Register agent card (capabilities)  │
│  3. Poll for tasks (5s interval)        │
│  4. For each task:                      │
│     a. POST /bridge → get worker JWT    │
│     b. GET SSE stream (read events)     │
│     c. Execute task locally             │
│     d. POST events (write results)      │
│     e. PUT status → completed           │
└─────────────────────────────────────────┘
```

## Quick Start

```
1. Web UI → workspace → "Add Agent" → copy registration code
2. POST /api/agent/register       (with code)  → tunnel_token
3. POST /api/agent/discovery/cards (with token) → register capabilities
4. Loop: GET /api/agent/tasks/poll              → receive tasks
5. For each task: bridge session → execute → write results
```

---

## Step 1: Agent Registration (one-time)

Generate a registration code from the Web UI or API, then register your agent.

### Generate Registration Code

```http
POST /api/workspaces/{workspace_id}/agent-code
Cookie: agentserver-token=<session_cookie>
```

Response:
```json
{
  "code": "abcdef123456789012345678",
  "expires_at": "2026-04-06T12:00:00Z"
}
```

The code is valid for 10 minutes.

### Register Agent

```http
POST /api/agent/register
Content-Type: application/json

{
  "code": "abcdef123456789012345678",
  "name": "iPhone 16 Pro Agent",
  "type": "claudecode"
}
```

Response (`201 Created`):
```json
{
  "sandbox_id": "884226d1-000c-40a5-99f0-d210a5cdbd6a",
  "tunnel_token": "2c1c67893d040f5d31b5d6cf7bbac338",
  "workspace_id": "9343b4a1-6420-462f-9520-334048b0cd33"
}
```

**Store these permanently on-device.** The `tunnel_token` is your authentication credential for all subsequent API calls.

| Field | Purpose | Storage |
|-------|---------|---------|
| `sandbox_id` | Your agent's unique ID | Keychain / SharedPreferences |
| `tunnel_token` | Auth token for all API calls | Keychain / SharedPreferences |
| `workspace_id` | Your workspace | Keychain / SharedPreferences |

---

## Step 2: Register Agent Card

Declare your agent's capabilities so other agents and users can discover it.

```http
POST /api/agent/discovery/cards
Authorization: Bearer <tunnel_token>
Content-Type: application/json

{
  "display_name": "iPhone 16 Pro Agent",
  "description": "Mobile agent with camera, location, and notification capabilities",
  "agent_type": "mobile",
  "card": {
    "skills": [
      {"name": "photo-capture", "description": "Take photos with device camera"},
      {"name": "location", "description": "Get current GPS location"},
      {"name": "notification", "description": "Send push notifications"},
      {"name": "sensor-data", "description": "Read device sensors (accelerometer, gyroscope)"}
    ],
    "tags": ["mobile", "ios", "camera", "location"],
    "platform": "ios",
    "model": "iPhone 16 Pro"
  }
}
```

Response (`200 OK`):
```json
{"status": "ok"}
```

Call this on every app launch to keep the card up-to-date.

---

## Step 3: Poll for Tasks

Poll the server for tasks assigned to your agent.

```http
GET /api/agent/tasks/poll?sandbox_id=<sandbox_id>
Authorization: Bearer <tunnel_token>
```

Response (`200 OK`):
```json
[
  {
    "task_id": "task_0bc1c113-4f09-4e1f-83f0-a937bc944be6",
    "session_id": "cse_3250e0b2-b2c9-480f-9b04-018d846eb73a",
    "prompt": "Take a photo of the whiteboard in the meeting room",
    "system_context": "",
    "max_turns": 0,
    "max_budget_usd": 0
  }
]
```

Empty array `[]` means no pending tasks. Poll every 5 seconds.

**Important**: The poll endpoint atomically marks tasks as `assigned` — once you receive a task, no other agent will receive it.

---

## Step 4: Execute Task via Bridge Session

For each received task, establish a bridge session to stream results back in real-time.

### 4a. Get Bridge Credentials

```http
POST /v1/agent/sessions/<session_id>/bridge
Authorization: Bearer <tunnel_token>
```

Response (`200 OK`):
```json
{
  "worker_jwt": "eyJhbGciOiJIUzI1NiJ9.eyJzaWQiOiJjc2VfMzI1MGU...",
  "api_base_url": "https://agent.cs.ac.cn/v1/agent/sessions/cse_3250e0b2-b2c9-480f-9b04-018d846eb73a",
  "expires_in": 86400,
  "worker_epoch": 1
}
```

| Field | Purpose |
|-------|---------|
| `worker_jwt` | Auth token for all `/worker/*` endpoints (24h TTL) |
| `api_base_url` | Base URL for worker endpoints — append `/worker/...` to this |
| `expires_in` | JWT lifetime in seconds |
| `worker_epoch` | Include this in every worker request; server returns 409 if stale |

> **CCR V2 alignment**: This endpoint is identical to Claude Code's `POST /v1/code/sessions/{id}/bridge`. Each call bumps the epoch — the `/bridge` call IS the worker registration.

### 4b. Report Worker State

Report that you're starting work:

```http
PUT <api_base_url>/worker
Authorization: Bearer <worker_jwt>
Content-Type: application/json

{
  "worker_status": "running",
  "worker_epoch": 1
}
```

Valid states: `idle`, `running`, `requires_action`.

### 4c. Start Heartbeat

Send heartbeats every 20 seconds to maintain your session. If the server doesn't receive a heartbeat for 60 seconds, the worker is considered dead.

```http
POST <api_base_url>/worker/heartbeat
Authorization: Bearer <worker_jwt>
Content-Type: application/json

{
  "session_id": "<session_id>",
  "worker_epoch": 1
}
```

**Implementation note**: Add ±10% jitter to the 20s interval (18-22s) to avoid thundering herd.

### 4d. Connect SSE Read Stream (optional)

If you need to receive inbound events (e.g., interrupt signals, permission prompts):

```http
GET <api_base_url>/worker/events/stream
Authorization: Bearer <worker_jwt>
Accept: text/event-stream
```

Optional query parameters:
- `from_sequence_num=N` — resume from sequence number N (skip already-seen events)

Optional headers:
- `Last-Event-ID: N` — same as `from_sequence_num` (SSE standard)

SSE frame format:
```
event: client_event
id: 42
data: {"event_id":"evt_xxx","sequence_num":42,"event_type":"client_event","source":"client","payload":{"type":"user","message":{"content":"take a photo"}},"created_at":"2026-04-06T12:00:00Z"}

:keepalive

event: client_event
id: 43
data: {"event_id":"evt_yyy","sequence_num":43,...}
```

- `:keepalive` comments are sent every 15 seconds — reset your liveness timer on them
- If no data (including keepalives) for 45 seconds, reconnect with `from_sequence_num` set to your last seen `id`

> **CCR V2 alignment**: Identical to Claude Code's `GET /worker/events/stream`. Same frame format, keepalive pattern, and sequence number resumption.

### 4e. Write Events (Task Results)

Stream your task results back through the bridge:

```http
POST <api_base_url>/worker/events
Authorization: Bearer <worker_jwt>
Content-Type: application/json

{
  "worker_epoch": 1,
  "events": [
    {
      "payload": {
        "type": "assistant",
        "uuid": "unique-id-1",
        "session_id": "<session_id>",
        "message": {
          "role": "assistant",
          "content": [
            {"type": "text", "text": "I've taken the photo. Here's what I see on the whiteboard..."}
          ]
        }
      }
    }
  ]
}
```

Rules:
- Max **100 events** per batch
- Max **10 MB** body size
- Each event payload **must have a `uuid` field** (for deduplication)
- Server returns **409** if `worker_epoch` doesn't match (a newer worker replaced you)

> **CCR V2 alignment**: Identical to Claude Code's `POST /worker/events`. Same batch limits, epoch validation, and UUID deduplication.

### 4f. Send Final Result

When the task is complete, send a result event:

```http
POST <api_base_url>/worker/events
Authorization: Bearer <worker_jwt>
Content-Type: application/json

{
  "worker_epoch": 1,
  "events": [
    {
      "payload": {
        "type": "result",
        "uuid": "unique-id-result",
        "session_id": "<session_id>",
        "subtype": "success",
        "result": "Photo captured and analyzed. The whiteboard contains...",
        "is_error": false,
        "duration_ms": 3200,
        "num_turns": 1
      }
    }
  ]
}
```

### 4g. Update Task Status

Mark the task as completed on the server:

```http
PUT /api/agent/tasks/<task_id>/status
Authorization: Bearer <tunnel_token>
Content-Type: application/json

{"status": "completed"}
```

Or if the task failed:
```json
{"status": "failed", "failure_reason": "Camera permission denied"}
```

### 4h. Report Idle State

```http
PUT <api_base_url>/worker
Authorization: Bearer <worker_jwt>
Content-Type: application/json

{
  "worker_status": "idle",
  "worker_epoch": 1
}
```

---

## Complete API Reference

### Authentication

Two auth mechanisms:

| Token | Obtained From | Used For | Header |
|-------|---------------|----------|--------|
| `tunnel_token` | `POST /api/agent/register` | Agent registration, card, task poll, task status | `Authorization: Bearer <tunnel_token>` |
| `worker_jwt` | `POST /v1/agent/sessions/{id}/bridge` | All `/worker/*` endpoints within a session | `Authorization: Bearer <worker_jwt>` |

### Agent Lifecycle Endpoints

#### POST /api/agent/register

Register a new agent.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `code` | string | yes | One-time registration code from Web UI |
| `name` | string | no | Display name (default: "Local Agent") |
| `type` | string | no | Agent type: `"claudecode"` or `"mobile"` (default: "opencode") |

Response `201`:
```json
{"sandbox_id": "...", "tunnel_token": "...", "workspace_id": "..."}
```

#### POST /api/agent/discovery/cards

Register or update agent capabilities.

Auth: `Bearer <tunnel_token>`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `display_name` | string | no | Agent display name (default: sandbox name) |
| `description` | string | no | Brief description for discovery |
| `agent_type` | string | no | Default: "claudecode" |
| `card` | object | no | Arbitrary JSON with skills, tags, etc. |

Response `200`: `{"status": "ok"}`

#### GET /api/agent/tasks/poll

Poll for pending tasks.

Auth: `Bearer <tunnel_token>`

Query params:
- `sandbox_id` — your sandbox ID (optional, derived from token)

Response `200`: Array of tasks (empty `[]` if none).

#### PUT /api/agent/tasks/{id}/status

Update task execution status.

Auth: `Bearer <tunnel_token>`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `status` | string | yes | `"running"`, `"completed"`, `"failed"`, `"cancelled"` |
| `failure_reason` | string | no | Error message (when status=failed) |

### Bridge Session Endpoints (CCR V2)

All paths relative to `{api_base_url}` returned by `/bridge`.

#### POST /v1/agent/sessions

Create a new bridge session.

Auth: `Bearer <tunnel_token>`

```json
{"title": "Task xyz", "bridge": {}, "sandbox_id": "<sandbox_id>"}
```

Response `201`:
```json
{"session": {"id": "cse_..."}}
```

#### POST /v1/agent/sessions/{id}/bridge

Exchange auth for worker JWT. Each call bumps epoch.

Auth: `Bearer <tunnel_token>`

Response `200`:
```json
{
  "worker_jwt": "...",
  "api_base_url": "https://host/v1/agent/sessions/cse_...",
  "expires_in": 86400,
  "worker_epoch": 1
}
```

#### POST /v1/agent/sessions/{id}/archive

Archive a session after task completion.

Auth: `Bearer <tunnel_token>`

Response `200`.

#### GET {api_base_url}/worker/events/stream

SSE event stream (read channel).

Auth: `Bearer <worker_jwt>`

Query: `from_sequence_num=N`

Headers: `Accept: text/event-stream`, `Last-Event-ID: N`

#### POST {api_base_url}/worker/events

Write events (write channel).

Auth: `Bearer <worker_jwt>`

```json
{
  "worker_epoch": 1,
  "events": [{"payload": {...}, "ephemeral": false}]
}
```

Max 100 events, 10 MB.

Errors: `409` epoch mismatch.

#### POST {api_base_url}/worker/internal-events

Write internal events (transcript storage, not visible to clients).

Auth: `Bearer <worker_jwt>`

```json
{
  "worker_epoch": 1,
  "events": [{"payload": {...}, "is_compaction": false, "agent_id": ""}]
}
```

#### POST {api_base_url}/worker/events/delivery

ACK event delivery.

Auth: `Bearer <worker_jwt>`

```json
{
  "worker_epoch": 1,
  "updates": [{"event_id": "...", "status": "received"}]
}
```

Status values: `received`, `processing`, `processed`.

#### PUT {api_base_url}/worker

Report worker state and metadata.

Auth: `Bearer <worker_jwt>`

```json
{
  "worker_status": "running",
  "worker_epoch": 1,
  "external_metadata": {"platform": "ios", "battery": 85}
}
```

States: `idle`, `running`, `requires_action`.

#### POST {api_base_url}/worker/heartbeat

Worker liveness heartbeat.

Auth: `Bearer <worker_jwt>`

```json
{"session_id": "cse_...", "worker_epoch": 1}
```

Send every 20s (±10% jitter). Server TTL: 60s.

#### GET {api_base_url}/worker

Read current worker state.

Auth: `Bearer <worker_jwt>`

Response:
```json
{
  "worker": {
    "state": "running",
    "external_metadata": {...},
    "last_heartbeat_at": "2026-04-06T12:00:00Z"
  }
}
```

#### GET {api_base_url}/worker/internal-events

Read internal events (for session resume).

Auth: `Bearer <worker_jwt>`

Query: `from_sequence_num=N`

---

## Error Handling

| HTTP Code | Meaning | Action |
|-----------|---------|--------|
| 200 | Success | Continue |
| 201 | Created | Continue |
| 400 | Bad request | Fix request body/params |
| 401 | Auth failed | Re-register or refresh token |
| 403 | Forbidden | Wrong sandbox/session |
| 404 | Not found | Session archived or invalid ID |
| 409 | Epoch mismatch | A newer worker replaced you. Stop, re-POST `/bridge` for new epoch |
| 500 | Server error | Retry with backoff |

### 409 Epoch Mismatch

This means another worker instance registered for the same session (e.g., app restart during task execution). You must:

1. Stop sending events
2. Stop heartbeat
3. Close SSE connection
4. Optionally re-POST `/bridge` to get a new epoch (this replaces the other worker)

### Reconnect Strategy

For SSE and HTTP POST failures:

```
Backoff: exponential 1s → 30s (cap), with ±25% jitter
Budget: give up after 10 minutes of consecutive failures
Permanent errors (401, 403, 404): don't retry, close immediately
```

---

## CCR V2 Protocol Alignment

This API is designed to be compatible with Claude Code's CCR V2 bridge protocol. The following table shows the alignment:

| CC Endpoint | agentserver Endpoint | Status |
|-------------|---------------------|--------|
| `POST /v1/code/sessions` | `POST /v1/agent/sessions` | Path differs (`code` → `agent`) |
| `POST /v1/code/sessions/{id}/bridge` | `POST /v1/agent/sessions/{id}/bridge` | Path differs, same semantics |
| `GET /worker/events/stream` | `GET /worker/events/stream` | Identical |
| `POST /worker/events` | `POST /worker/events` | Identical |
| `POST /worker/internal-events` | `POST /worker/internal-events` | Identical |
| `POST /worker/events/delivery` | `POST /worker/events/delivery` | Identical (agentserver: informational only) |
| `PUT /worker` | `PUT /worker` | Identical |
| `POST /worker/heartbeat` | `POST /worker/heartbeat` | Identical |
| `GET /worker` | `GET /worker` | Identical |

**Key differences from CC**:

| Aspect | Claude Code | agentserver |
|--------|-------------|-------------|
| Auth for session creation | OAuth Bearer token | `tunnel_token` from agent registration |
| Auth for worker endpoints | Opaque JWT from `/bridge` | HMAC-SHA256 JWT from `/bridge` |
| Task dispatch | MCP tools (delegate_task) | HTTP poll (`GET /api/agent/tasks/poll`) |
| Message filtering | Only eligible messages forwarded | All events forwarded |
| Delivery ACK | Active lifecycle tracking | Informational (no-op) |
| Worker state | Coalesced PUT (1 pending max) | Atomic PUT per call |

**Fully compatible aspects**:

- SSE frame format (`event: client_event\nid: N\ndata: {...}\n\n`)
- Sequence number tracking and resumption (`from_sequence_num`, `Last-Event-ID`)
- Epoch management (bump on `/bridge`, validate on writes, 409 on mismatch)
- Event batching (max 100/batch, 10MB)
- UUID deduplication
- Heartbeat interval (20s) and TTL (60s)
- Keepalive comments (`:keepalive` every 15s)
- Worker JWT lifecycle (24h TTL, re-POST `/bridge` to refresh)

---

## iOS Implementation Checklist

- [ ] Store `sandbox_id`, `tunnel_token`, `workspace_id` in Keychain
- [ ] Register agent card on app launch
- [ ] Background task for polling (`BGAppRefreshTask`, 5s when foreground)
- [ ] `URLSession` with `streamTask` for SSE
- [ ] Heartbeat timer (20s ± jitter) — use `DispatchSourceTimer`
- [ ] Handle 409 epoch mismatch (stop + re-bridge)
- [ ] Handle app backgrounding (pause heartbeat, resume on foreground)
- [ ] Exponential backoff for retries (1s → 30s)

## Android Implementation Checklist

- [ ] Store credentials in EncryptedSharedPreferences
- [ ] Register agent card on app launch
- [ ] `WorkManager` periodic task for polling (foreground: 5s, background: 15min)
- [ ] `OkHttp` EventSource for SSE
- [ ] `Handler.postDelayed` for heartbeat (20s ± jitter)
- [ ] Handle 409 epoch mismatch
- [ ] Handle Doze mode (batch heartbeats with `AlarmManager.setExactAndAllowWhileIdle`)
- [ ] Exponential backoff for retries

---

## Example: Complete Task Execution Flow (pseudocode)

```
// 1. Poll for tasks
tasks = GET /api/agent/tasks/poll?sandbox_id={sandbox_id}
        Authorization: Bearer {tunnel_token}

if tasks is empty: return

task = tasks[0]

// 2. Get bridge credentials
creds = POST /v1/agent/sessions/{task.session_id}/bridge
        Authorization: Bearer {tunnel_token}

jwt = creds.worker_jwt
base = creds.api_base_url
epoch = creds.worker_epoch

// 3. Report running state
PUT {base}/worker
    Authorization: Bearer {jwt}
    Body: {"worker_status": "running", "worker_epoch": epoch}

// 4. Start heartbeat (background timer, 20s)
startHeartbeat(base, jwt, epoch)

// 5. Execute task
result = executeTaskLocally(task.prompt)

// 6. Write result events
POST {base}/worker/events
     Authorization: Bearer {jwt}
     Body: {
       "worker_epoch": epoch,
       "events": [
         {"payload": {"type": "assistant", "uuid": uuid(), "message": {"role": "assistant", "content": [{"type": "text", "text": result}]}}},
         {"payload": {"type": "result", "uuid": uuid(), "subtype": "success", "result": result, "is_error": false}}
       ]
     }

// 7. Report idle
PUT {base}/worker
    Body: {"worker_status": "idle", "worker_epoch": epoch}

// 8. Stop heartbeat
stopHeartbeat()

// 9. Update task status
PUT /api/agent/tasks/{task.task_id}/status
    Authorization: Bearer {tunnel_token}
    Body: {"status": "completed"}
```
