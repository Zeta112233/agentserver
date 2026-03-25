# Multi-Agent Discovery, Capability Awareness & Interaction

**Date**: 2026-03-25
**Status**: Draft
**Scope**: Workspace-scoped multi-agent discovery and interaction for agentserver

## Problem Statement

Agents (local opencode, cloud opencode/openclaw/nanoclaw) in the same workspace are currently isolated — they cannot discover each other, understand each other's capabilities, or collaborate on tasks. This limits the system to independent, siloed agents that cannot leverage specialized capabilities of their peers.

## Goals

1. **Discovery**: Any agent in a workspace can discover all other agents in the same workspace
2. **Capability awareness**: Agents can understand what each peer agent can do (skills, tools, capacity)
3. **Interaction**: Agents can delegate tasks to appropriate peers and receive results
4. **Hybrid autonomy**: Simple delegations happen autonomously; complex ones surface to the human for approval
5. **Availability-aware**: System gracefully handles agents going offline (especially local agents)

## Non-Goals

- Cross-workspace agent discovery (future work)
- External/third-party agent federation
- Full A2A protocol compliance (we borrow concepts but don't implement the full spec)
- Real-time streaming of intermediate results between agents (future work)

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Server-centric registry | Extends existing agentserver hub; minimal new infrastructure; full observability |
| Discovery protocol | A2A-inspired Agent Cards | Industry-aligned structured capability description |
| Capability invocation | MCP tool proxying | Standard protocol; agents already understand MCP tools |
| Communication path | Server-mediated + K8s direct fast-path | Balance of control and performance |
| Interaction scope | Same workspace only | Matches existing K8s namespace isolation |
| Local agent participation | Bidirectional with availability | Local agents can both discover and be discovered; heartbeat tracks availability |
| Human involvement | Hybrid (auto + approval modes) | AI decides simple delegations; surfaces complex ones to human |

## Competitive Landscape

The design draws from industry research across major multi-agent frameworks:

| Framework | Discovery | Interaction | Orchestration |
|-----------|-----------|-------------|---------------|
| **Google A2A** | Agent Card (JSON) at endpoints | JSON-RPC 2.0 sync/stream/push | Peer-to-peer protocol |
| **Anthropic MCP** | `*/list` methods + change notifications | JSON-RPC 2.0 tool calls | Host-to-server |
| **Microsoft AutoGen** | Static registration + descriptions | Direct messaging + pub/sub | Round-robin / selector / swarm |
| **CrewAI** | Static YAML config | Task output as context | Sequential / hierarchical |
| **LangGraph** | Static graph nodes | Shared state mutation | Graph edges + conditionals |
| **OpenAI Swarm** | Static code-level | Function returns + handoffs | Decentralized per-agent |
| **MetaGPT** | Implicit via pub/sub topology | Publish/subscribe messages | SOP-defined |

**Key takeaways informing this design:**
- A2A Agent Card is the emerging standard for agent capability description
- MCP is the standard for tool/capability invocation
- Supervisor/router is the most battle-tested production pattern
- Formal capability schemas are more reliable than description-based routing
- No framework has fully solved dynamic discovery — all use some form of registry

---

## Interaction Model Clarification

This system provides **two distinct interaction layers** that serve different purposes:

| Layer | Tools | Who Uses It | Purpose |
|-------|-------|-------------|---------|
| **Task Delegation** | `discover_agents`, `delegate_task`, `check_task` | AI model (via injected MCP tools) | High-level: "review this code", "translate this document" |
| **MCP Tool Proxy** | `agent/{id}/tool_name` | Task executors (internal infrastructure) | Low-level: "read a file on agent A's filesystem", "run a command on agent B" |

**Task Delegation** is the primary, AI-model-facing interaction. The AI model calls `discover_agents` to find peers, `delegate_task` to send work, and `check_task` to retrieve results. These three tools are all the AI model needs.

**MCP Tool Proxy** is internal infrastructure used by task executors during task execution. For example, when Agent B receives a code review task, its task executor might need to read files from Agent A's filesystem — it does this via the MCP proxy, calling `agent/{agent-a-id}/read_file`. This is a programmatic call made by the task executor, not by the AI model directly. Remote agent tools are NOT injected into the AI model's tool list.

## Data Access: How Agents Share Files

Agents have isolated filesystems (separate containers or machines). When Agent A delegates a task to Agent B that involves files, the data access mechanism depends on the deployment topology:

**Cloud agents in the same workspace:**
- **Shared workspace volumes**: The existing `workspace_volumes` table supports mounting K8s PVCs to multiple sandboxes. All cloud agents in a workspace can mount the same volume at a configured path, giving them shared read/write access to a common filesystem.
- This is the primary mechanism for cloud-to-cloud collaboration.

**Cross-environment (local→cloud or cloud→local):**
- **MCP Tool Proxy callback**: During task execution, the target agent's task executor calls back to the requester's `read_file` tool via the MCP proxy to fetch needed files. This is transparent to the AI model.
- **Task payload embedding**: For small inputs (code snippets, text), the file content can be embedded directly in the task's `input` field.
- **Git-based sharing**: For code review tasks, the task input can include a git remote URL + branch/commit. The target agent clones the repo independently.

**The task input schema should indicate which data access method to use:**
```json
{
  "skill": "code-review",
  "input": {
    "data_access": "shared_volume",    // or "callback" or "embedded" or "git"
    "path": "/workspace/src/server",   // for shared_volume
    "git_url": "...",                  // for git
    "git_ref": "main",                // for git
    "files": { "main.go": "..." },    // for embedded
    "focus": "bugs and security"
  }
}
```

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                     Workspace                           │
│                                                         │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐           │
│  │ Local     │   │ Cloud    │   │ Cloud    │           │
│  │ Opencode  │   │ Opencode │   │ NanoClaw │           │
│  │           │   │          │   │          │           │
│  │ AgentCard │   │ AgentCard│   │ AgentCard│           │
│  │ MCP Tools │   │ MCP Tools│   │ MCP Tools│           │
│  └─────┬─────┘   └─────┬────┘   └─────┬────┘           │
│        │ WS Tunnel      │ HTTP         │ HTTP           │
│        │                │              │                │
│  ┌─────┴────────────────┴──────────────┴────────┐      │
│  │              AgentServer                      │      │
│  │                                               │      │
│  │  ┌─────────────────┐  ┌───────────────────┐  │      │
│  │  │  Agent Card      │  │  Task Manager     │  │      │
│  │  │  Registry        │  │  (lifecycle,      │  │      │
│  │  │  (discovery,     │  │   delegation,     │  │      │
│  │  │   matching)      │  │   routing)        │  │      │
│  │  └─────────────────┘  └───────────────────┘  │      │
│  │                                               │      │
│  │  ┌─────────────────┐  ┌───────────────────┐  │      │
│  │  │  MCP Proxy       │  │  Audit Logger     │  │      │
│  │  │  (tool call      │  │  (interactions,   │  │      │
│  │  │   forwarding)    │  │   metrics)        │  │      │
│  │  └─────────────────┘  └───────────────────┘  │      │
│  └──────────────────────────────────────────────┘      │
│                                                         │
│  Cloud agents: direct fast-path within K8s namespace    │
│  ┌──────────┐ ──── MCP direct ────> ┌──────────┐      │
│  │ Cloud    │ (pod-to-pod, JWT auth) │ Cloud    │      │
│  │ Opencode │ <──────────────────── │ NanoClaw │      │
│  └──────────┘                       └──────────┘      │
└─────────────────────────────────────────────────────────┘
```

---

## Component Design

### 1. Agent Card Schema

The Agent Card is the core data structure for capability description. Every sandbox publishes a card.

```go
// AgentCard represents an agent's capabilities and metadata.
// When stored in DB: Type and Status live in indexed columns, NOT in card_json.
// When returned via API: the server merges columns + JSON into this struct.
type AgentCard struct {
    // Identity (Type comes from agent_cards.agent_type column, not JSON)
    AgentID     string    `json:"agent_id"`       // sandbox ID
    Name        string    `json:"name"`            // human-readable name
    Type        string    `json:"type"`            // "opencode" | "openclaw" | "nanoclaw" (from column)
    Description string    `json:"description"`     // what this agent does

    // Capabilities (stored in card_json)
    Skills      []Skill   `json:"skills"`          // high-level skill descriptions
    MCPTools    []MCPTool `json:"mcp_tools"`       // MCP tools this agent exposes

    // Availability (Status comes from agent_cards.agent_status column, not JSON)
    Status      string    `json:"status"`          // "available" | "busy" | "offline" (from column)
    IsLocal     bool      `json:"is_local"`        // local vs cloud agent
    LastSeenAt  time.Time `json:"last_seen_at"`    // last heartbeat (from agent_info table)

    // Interaction (stored in card_json)
    SupportedModes []string `json:"supported_modes"` // ["sync", "async", "stream"]
    MaxConcurrency int     `json:"max_concurrency"`  // how many tasks simultaneously
}

type Skill struct {
    Name        string   `json:"name"`         // e.g., "code-review", "translation"
    Description string   `json:"description"`  // what this skill does
    InputTypes  []string `json:"input_types"`  // e.g., ["text", "file", "code"]
    OutputTypes []string `json:"output_types"`
    Tags        []string `json:"tags"`         // for matching: ["go", "python", "frontend"]
}

type MCPTool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"input_schema"` // JSON Schema
}
```

**Design rationale**: The schema is inspired by A2A's Agent Card but simplified. Skills provide high-level capability matching (what can this agent do?), while MCPTools provide the fine-grained invocation interface (how do I call it?). The separation allows discovery queries to be fast (match on skills/tags) while detailed tool schemas are loaded on demand.

### 2. Agent Card Registry

**Storage:**

```sql
CREATE TABLE agent_cards (
    sandbox_id TEXT PRIMARY KEY REFERENCES sandboxes(id) ON DELETE CASCADE,
    agent_type TEXT NOT NULL,                    -- indexed: "opencode" | "openclaw" | "nanoclaw"
    agent_status TEXT NOT NULL DEFAULT 'available', -- indexed: "available" | "busy" | "offline"
    card_json  TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agent_cards_type ON agent_cards(agent_type);
CREATE INDEX idx_agent_cards_status ON agent_cards(agent_status);
```

**Design note**: `agent_type` and `agent_status` are indexed columns that live OUTSIDE the JSON blob. The `agent_status` column is the authoritative source of truth for availability status — it is updated both by agent heartbeats and by the `AgentHealthMonitor`. The `card_json` does NOT contain `status` or `type` fields; these are always read from the columns. When constructing an `AgentCard` API response, the server merges the column values with the JSON blob.

**Registration flow:**
1. Agent starts up and determines its capabilities (type defaults + custom config)
2. Agent reports its card to the server:
   - Cloud agents: The `agentserver-mcp-bridge` process calls `POST /api/agent/discovery/cards` during startup. It knows the agentserver URL via the `AGENTSERVER_URL` environment variable (injected during container creation alongside existing env vars like `ANTHROPIC_BASE_URL` and proxy tokens).
   - Local agents: Send card via WebSocket tunnel alongside `agent_info`
3. Server stores the card in `agent_cards` table
4. On capability change, agent re-reports with incremented version
5. Server marks agents as `offline` when heartbeat lapses (60s threshold)

**Default cards by type:**
- The server pre-populates default skills and tools based on agent type
- Agents can extend or override defaults
- This means even agents that don't explicitly register a card will have basic discoverability

### 3. Discovery API

**Authentication**: All agent-to-server API calls use a new route group `/api/agent/` that authenticates via `proxy_token` in the `Authorization` header (e.g., `Authorization: Bearer <proxy_token>`). This is separate from the existing cookie-based `/api/` routes used by the web UI. A new `AgentAuthMiddleware` validates the proxy_token, resolves the sandbox, and injects the sandbox context into the request.

**Endpoints:**

```
GET  /api/agent/discovery/agents
     Auth: proxy_token (AgentAuthMiddleware)
     Query params:
       ?type=opencode          — filter by agent type
       ?status=available       — filter by availability
       ?skill=code-review      — filter by skill name
       ?tag=go                 — filter by skill tag
       ?limit=10               — max results (default 10, max 50)
     Response: {
       "agents": [AgentCard, ...],
       "total": 15
     }
     Note: workspace is resolved from the authenticated sandbox's workspace_id.
           The requesting agent is always excluded from results (no self-discovery).

GET  /api/agent/discovery/agents/{sandbox_id}
     Auth: proxy_token (AgentAuthMiddleware)
     Response: AgentCard (full, including MCP tool schemas)

POST /api/agent/discovery/cards
     Auth: proxy_token (AgentAuthMiddleware)
     Body: AgentCard
     Response: { "version": N }
     Note: sandbox_id is resolved from the authenticated proxy_token, not from the body
```

**Capability matching algorithm:**

When an agent queries for a capability, the server returns matching agents ranked by priority order (not a weighted score — the ranking is a stable sort applying these criteria in order):

1. **Availability** (filter) — `offline` agents are excluded entirely; `available` sorted before `busy`
2. **Skill match** (sort) — exact skill name match > tag overlap > type-inferred match
3. **Locality** (sort) — prefer cloud↔cloud or local↔local for lower latency
4. **Load** (tiebreaker) — fewer active tasks preferred

The implementation may evolve the ranking formula, but the priority order above is the contract.

**Injected MCP tool:**

```json
{
  "name": "discover_agents",
  "description": "Discover other agents in this workspace by skill, tags, or type. Returns up to 10 available agents with their capabilities.",
  "input_schema": {
    "type": "object",
    "properties": {
      "skill": { "type": "string", "description": "Skill name to search for (e.g., 'code-review')" },
      "tags": { "type": "array", "items": { "type": "string" }, "description": "Tags to match (e.g., ['go', 'python'])" },
      "type": { "type": "string", "enum": ["opencode", "openclaw", "nanoclaw"], "description": "Filter by agent type" },
      "status": { "type": "string", "enum": ["available", "busy"], "description": "Filter by availability status" },
      "limit": { "type": "integer", "description": "Max results to return (default 10)", "default": 10 }
    }
  }
}
```

### 4. Task Lifecycle & Delegation

**Task states:** `pending` → `accepted` → `running` → `completed` | `failed` | `rejected`

**Storage:**

```sql
CREATE TABLE agent_tasks (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    requester_id TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    skill TEXT,
    input_json TEXT NOT NULL,
    output_json TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    mode TEXT NOT NULL DEFAULT 'async',
    failure_reason TEXT,
    timeout_seconds INTEGER DEFAULT 300,
    delegation_chain TEXT NOT NULL DEFAULT '[]',  -- JSON array of sandbox IDs for loop detection
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accepted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_tasks_workspace ON agent_tasks(workspace_id);
CREATE INDEX idx_agent_tasks_requester ON agent_tasks(requester_id);
CREATE INDEX idx_agent_tasks_target_status ON agent_tasks(target_id, status);
CREATE INDEX idx_agent_tasks_cleanup ON agent_tasks(status, completed_at);
```

**Delegation endpoints:**

```
POST /api/agent/tasks
     Auth: proxy_token (AgentAuthMiddleware, resolves requester sandbox)
     Body: {
       "target_id": "sandbox-b-id",         // specific target, OR
       "target_skill": "code-review",        // let server pick best match
       "input": { ... },                     // task payload
       "mode": "sync" | "async",
       "timeout": 300                        // seconds (max 600, default 300)
     }
     Response (async): { "task_id": "...", "status": "pending" }
     Response (sync):  blocks until completion or timeout, then:
                       { "task_id": "...", "status": "completed", "output": {...} }
                       On timeout: HTTP 408 { "task_id": "...", "status": "failed",
                                              "failure_reason": "timeout" }

GET  /api/agent/tasks/{task_id}
     Auth: proxy_token
     Response: full task record

GET  /api/agent/tasks?status=running
     Auth: proxy_token (filters to requester's tasks)
     Response: { "tasks": [...] }
```

**Sync mode implementation**: The server holds the HTTP connection open using a blocking channel. A background goroutine monitors task status changes. The connection has a hard timeout of `min(request.timeout, 600)` seconds. If the HTTP connection drops mid-sync, the task continues running — the requester can poll via `GET /api/agent/tasks/{task_id}` to retrieve the result later.

**Task delivery to target agent:**

Each agent type must expose a task reception endpoint. The server delivers tasks to this endpoint.

**Cloud agents** — HTTP POST to `http://{pod_ip}:{task_port}/agent/tasks`:
```json
// Request from server to cloud agent
POST /agent/tasks
Content-Type: application/json
Authorization: Bearer <internal_task_token>

{
  "task_id": "task-123",
  "requester_id": "sandbox-a",
  "requester_name": "Local Opencode",
  "skill": "code-review",
  "input": { "path": "/src/server", "focus": "bugs" }
}

// Response from cloud agent (immediate)
HTTP 200
{
  "status": "accepted"   // or "rejected" with "reason"
}
```

**Local agents** — forwarded via WebSocket tunnel using new frame types:

```
Frame type: "task_request" (server → agent)
Payload: {
  "task_id": "task-123",
  "requester_id": "sandbox-a",
  "requester_name": "Local Opencode",
  "skill": "code-review",
  "input": { ... }
}

Frame type: "task_response" (agent → server)
Payload: {
  "task_id": "task-123",
  "status": "accepted" | "rejected" | "running" | "completed" | "failed",
  "output": { ... },         // present when status is "completed"
  "failure_reason": "..."    // present when status is "rejected" or "failed"
}
```

The agent sends multiple `task_response` frames as the task progresses through its lifecycle (accepted → running → completed/failed). The server updates the `agent_tasks` table on each status change.

**Target-side execution model:**

When an agent receives a task, it must execute it within its own environment:

| Agent Type | Execution Method |
|------------|-----------------|
| **opencode** | Injects the task as a new prompt into the opencode conversation via its `/api/chat` endpoint. The task input becomes a system-contextualized user message. The opencode instance runs the task autonomously and reports results. |
| **openclaw** | Routes the task through the openclaw gateway as a model request. The skill name maps to a preconfigured prompt template. |
| **nanoclaw** | Forwards the task to the nanoclaw bridge via the existing HTTP bridge interface. The bridge handles autonomous execution. |

Each agent type needs a **task executor** component that:
1. Receives the task from the server (via HTTP endpoint or tunnel frame)
2. Translates the task input into the agent's native execution format
3. Runs the task
4. Reports status transitions back to the server
5. Returns the final output

This task executor is new infrastructure that must be built for each agent type. For cloud agents, it's a lightweight HTTP server running alongside the main process. For local agents, it's integrated into the agent CLI process that manages the WebSocket tunnel.

**Delegation loop detection:**

Each task carries a `delegation_chain` field — an ordered list of agent IDs that have participated in the delegation chain. When Agent A creates a task, the chain is `["agent-a"]`. If Agent B receives the task and decides to sub-delegate, the new task's chain becomes `["agent-a", "agent-b"]`. Before creating a task, the server checks: if `target_id` already appears in the `delegation_chain`, the task is rejected with reason `delegation_loop_detected`. The chain has a max depth of 5 to prevent excessively deep delegation trees.

```sql
-- Add to agent_tasks table:
delegation_chain TEXT DEFAULT '[]',  -- JSON array of sandbox IDs
```

**Task overflow handling:**

When an agent is at `max_concurrency` (status `busy`), new task requests are **rejected** with status `rejected` and reason `at_capacity`. The server does NOT queue tasks — the requester is expected to:
1. Try a different agent via `discover_agents` (which excludes `busy` agents by default)
2. Wait and retry later
3. Report to the user that no agent is available

This keeps the system simple and avoids hidden queuing complexity.

**Task cleanup:**

Completed and failed tasks are retained for 7 days, then automatically deleted by a background goroutine (`TaskCleanupWorker`) that runs daily. This prevents unbounded growth of the `agent_tasks` table. The `agent_interactions` audit table is NOT cleaned up — it serves as a permanent audit trail.

**Human-in-the-loop mechanisms:**

1. **Confidence-based surfacing**: The AI model decides when to ask the human. The system provides the tools; the model's own judgment determines when a delegation is "obvious" vs. "needs confirmation."

2. **Workspace delegation mode**: Configurable per workspace:
   - `auto`: Tasks are delivered to targets immediately
   - `approval`: Tasks are held in `pending` status until a workspace member approves via the UI

**Injected MCP tools:**

```json
{
  "name": "delegate_task",
  "description": "Delegate a task to another agent in this workspace. Use discover_agents first to find suitable agents.",
  "input_schema": {
    "type": "object",
    "properties": {
      "target_id": { "type": "string", "description": "Target agent's sandbox ID" },
      "skill": { "type": "string", "description": "Skill to invoke on the target agent" },
      "input": { "type": "object", "description": "Task input data" },
      "mode": { "type": "string", "enum": ["sync", "async"], "default": "async" }
    },
    "required": ["target_id", "input"]
  }
}
```

```json
{
  "name": "check_task",
  "description": "Check the status and result of a previously delegated task.",
  "input_schema": {
    "type": "object",
    "properties": {
      "task_id": { "type": "string", "description": "Task ID returned by delegate_task" }
    },
    "required": ["task_id"]
  }
}
```

### 5. MCP Proxy & Tool Namespacing

> **Note**: The MCP Proxy is internal infrastructure used by task executors during task execution. It is NOT exposed to the AI model as injected tools. The AI model interacts only via `discover_agents`, `delegate_task`, and `check_task`. See the "Interaction Model Clarification" section above.

**Prerequisite: Agent-side MCP server**

Current agents do not expose MCP server endpoints. Each agent type needs a lightweight MCP tool execution server:

| Agent Type | MCP Server Implementation |
|------------|--------------------------|
| **opencode** | A sidecar HTTP server (or an extension to opencode's existing HTTP server on port 4096) that exposes `POST /mcp/tools/call`. It translates MCP tool calls into opencode's internal APIs (file read/write, terminal, search). |
| **openclaw** | An extension to the openclaw gateway (port 18789) that exposes `POST /mcp/tools/call`. It maps MCP tool calls to the gateway's existing model routing and plugin system. |
| **nanoclaw** | An extension to the nanoclaw bridge HTTP interface that exposes `POST /mcp/tools/call`. It maps MCP tool calls to the bridge's task execution API. |

The MCP tool execution endpoint follows a simple protocol:

```
POST /mcp/tools/call
Authorization: Bearer <internal_task_token>
Content-Type: application/json

{
  "tool_name": "read_file",
  "arguments": { "path": "/src/main.go" }
}

Response:
{
  "result": { "content": "package main\n..." },
  "error": null
}
```

This is new infrastructure that must be built per agent type. It is a Phase 3 deliverable.

**Tool namespacing:**

When agent A discovers agent B's MCP tools, the tools are presented with a namespace prefix using the sandbox ID (guaranteed unique) and `/` separator:

```
agent/{sandbox_id}/tool_name
```

Example: If agent `abc123` has a tool called `analyze_code`, agent A sees it as `agent/abc123/analyze_code`.

**Design note**: Sandbox ID is used instead of agent name because IDs are guaranteed unique within the system. The `/` separator is used instead of `::` to avoid conflicts with MCP protocol conventions.

**MCP proxy flow:**

```
Agent A calls tool "agent/abc123/analyze_code"
  → Server parses "agent/{id}/{tool}" prefix, resolves target sandbox abc123
  → Server validates: both agents in same workspace? requester authorized?
  → Server forwards tool call to target agent's MCP endpoint:
      - Local agents: via WebSocket tunnel (new frame type "mcp_tool_call" / "mcp_tool_result")
      - Cloud agents: via HTTP POST to pod-ip:port/mcp/tools/call
  → Target agent executes the tool via its MCP server
  → Server relays result back to Agent A
  → Server logs the interaction to audit table
```

**Direct fast-path (K8s only):**

For cloud-to-cloud MCP tool calls within the same K8s namespace:

1. Agent A calls a namespaced MCP tool
2. Server checks: both agents are cloud, same namespace
3. Server issues a short-lived JWT (5min TTL) signed with an HMAC-SHA256 symmetric key:
   ```json
   {
     "requester_id": "sandbox-a",
     "target_id": "sandbox-b",
     "workspace_id": "ws-123",
     "allowed_tools": ["analyze_code", "run_tests"],
     "jti": "unique-jwt-id",
     "exp": 1711382400
   }
   ```
4. Server returns target's pod IP + MCP port + JWT to Agent A
5. Agent A calls target directly at `pod-ip:mcp-port/mcp/tools/call` with JWT in Authorization header
6. Server logs interaction metadata (but doesn't proxy the payload)

**JWT signing and validation:**
- **Signing key**: A symmetric HMAC-SHA256 key, generated by the agentserver at startup and distributed to cloud agents via the `AGENTSERVER_JWT_SECRET` environment variable during container creation
- **Target validation**: The target agent validates the JWT signature using the shared secret, checks `exp`, verifies `target_id` matches itself, and checks `allowed_tools` contains the requested tool
- **Revocation**: JWTs include a `jti` (JWT ID). If an agent is terminated, its proxy_token is invalidated — the 5-minute TTL provides a bounded window. For immediate revocation needs, agents can optionally call `GET /api/agent/jwt/validate/{jti}` on the server, but this is not required for normal operation

### 6. Availability & Health

**Extended heartbeat:**

The existing 20-second agent_info heartbeat is extended to include:
- `card_version`: Current Agent Card version (server re-fetches card if version changed)
- `agent_status`: `"available"` or `"busy"` (based on current task load)
- `active_tasks`: Number of tasks currently being executed

**Status transitions:**

| From | To | Trigger |
|------|----|---------|
| `available` | `busy` | Active tasks reach `max_concurrency` |
| `busy` | `available` | Active tasks drop below `max_concurrency` |
| `available`/`busy` | `offline` | No heartbeat for 60 seconds (server-computed) |
| `offline` | `available` | Heartbeat resumes |

**Offline detection mechanism:**

The server runs a background goroutine (`AgentHealthMonitor`) that sweeps the `agent_cards` table every 30 seconds. For each agent with `agent_status != 'offline'`, it checks:
- Cloud agents: `last_heartbeat_at` from `agent_info` table (or pod health from K8s API)
- Local agents: `last_heartbeat_at` from tunnel connection

If `now() - last_heartbeat_at > 60s`, the agent is marked `offline`. This is a background sweep, not computed at query time, to ensure consistent status across all concurrent queries.

**Offline handling:**
1. `AgentHealthMonitor` marks agent as `offline` in the `agent_cards` table
2. Agent is excluded from discovery results
3. Pending/running tasks assigned to the offline agent are marked `failed` with reason `agent_offline`
4. Requesters are notified:
   - If sync mode: HTTP response returns immediately with error
   - If async mode: Task status updates to `failed`, requester discovers via `check_task`

### 7. Security

**Authentication:**

| Operation | Auth Method | Validation |
|-----------|-------------|------------|
| Discovery (list agents) | Requester's `proxy_token` | Token valid + agent belongs to workspace |
| Card registration | Agent's `proxy_token` or tunnel auth | Token valid + sandbox matches |
| Task creation | Requester's `proxy_token` | Both agents in same workspace |
| MCP proxy call | Requester's `proxy_token` | Both agents in same workspace + tool exists |
| Direct fast-path | Short-lived JWT (5min) | JWT signature + workspace match + tool allowlist |

**Audit logging:**

```sql
-- Intentionally no REFERENCES/FK constraints: audit records must survive entity deletion
-- (sandboxes, workspaces can be deleted while their interaction history is retained)
CREATE TABLE agent_interactions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    requester_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    interaction_type TEXT NOT NULL,  -- "discovery" | "task_create" | "task_complete" | "mcp_tool_call"
    detail_json TEXT,               -- request metadata (not full payloads)
    status TEXT NOT NULL,           -- "success" | "failed" | "rejected"
    duration_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agent_interactions_workspace_time ON agent_interactions(workspace_id, created_at);
```

### 8. Agent-Type Integration

**Injected tools per agent type:**

All agent types receive three injected MCP tools: `discover_agents`, `delegate_task`, `check_task`. These are injected via the agent's configuration mechanism:

| Agent Type | Injection Method | How It Works |
|------------|-----------------|--------------|
| **opencode** | `OPENCODE_CONFIG_CONTENT` env var | Add an MCP server entry pointing to the agentserver bridge process (see below) |
| **openclaw** | `openclaw.json` gateway config | Add as a built-in plugin/channel in the gateway configuration |
| **nanoclaw** | Environment variables | Set `AGENTSERVER_TASK_API_URL` and `AGENTSERVER_TASK_API_TOKEN` for the bridge to call |
| **local agents** | WebSocket tunnel `inject_tools` frame | See below for frame specification |

**Agentserver Bridge MCP Server:**

The three injected tools (`discover_agents`, `delegate_task`, `check_task`) need to be callable by the AI model via the MCP protocol. Since the actual implementation is HTTP calls to agentserver's `/api/agent/` endpoints, a **bridge MCP server** is needed to translate MCP tool calls → HTTP requests.

For **cloud agents**: A lightweight bridge process (`agentserver-mcp-bridge`) is started alongside the main agent process in the container. It:
1. Exposes a stdio-based MCP server (stdin/stdout JSON-RPC)
2. Receives tool calls from the AI model via the MCP protocol
3. Translates them to HTTP requests to `$AGENTSERVER_URL/api/agent/...` with the `proxy_token`
4. Returns the HTTP response as the MCP tool result

The bridge is configured in opencode's MCP server list via `OPENCODE_CONFIG_CONTENT`:
```json
{
  "mcpServers": {
    "agentserver": {
      "command": "/usr/local/bin/agentserver-mcp-bridge",
      "env": {
        "AGENTSERVER_URL": "https://...",
        "AGENTSERVER_TOKEN": "<proxy_token>"
      }
    }
  }
}
```

For **local agents**: The local agent CLI process (`agentserver-agent`) itself acts as the bridge. When it receives `inject_tools` frames, it starts a local stdio MCP server that opencode connects to (see frame spec below).

**`inject_tools` tunnel frame specification:**

The server sends an `inject_tools` text frame to local agents via the WebSocket tunnel. This frame is sent:
1. Once immediately after tunnel connection is established
2. Again whenever the workspace's agent topology changes (new agents join, agents go offline, capabilities change)

```
Frame type: "inject_tools" (server → agent, text frame)
Payload: {
  "tools": [
    {
      "name": "discover_agents",
      "description": "Discover other agents in this workspace...",
      "input_schema": { ... }
    },
    {
      "name": "delegate_task",
      "description": "Delegate a task to another agent...",
      "input_schema": { ... }
    },
    {
      "name": "check_task",
      "description": "Check the status of a delegated task...",
      "input_schema": { ... }
    }
  ],
  "api_base_url": "https://agentserver.example.com/api/agent",
  "auth_token": "<proxy_token>"
}
```

**Agent-side handling**: The local agent CLI (`internal/agent/client.go`) must be extended to:
1. Parse `inject_tools` frames (new handler alongside existing `agent_info` handling)
2. Register the tools with the running opencode instance via its configuration API
3. Implement an HTTP handler that the opencode instance calls when these tools are invoked, which proxies the call to the agentserver's `/api/agent/` endpoints

**Default Agent Cards by type:**

| Agent Type | Default Skills | Default MCP Tools |
|------------|---------------|-------------------|
| **opencode** | `code-editing` (tags: all installed languages), `terminal` (tags: bash, shell), `code-search` (tags: grep, find) | `read_file`, `edit_file`, `run_command`, `search_code` |
| **openclaw** | `text-generation` (tags: multi-model), `model-routing` (tags: anthropic, openai) | `generate_text`, `summarize`, `translate`, `list_models` |
| **nanoclaw** | `autonomous-task` (tags: configurable), `knowledge-query` | `run_task`, `query_knowledge` |

Agents extend defaults with custom skills and tools based on their specific configuration.

---

## End-to-End Example

**Scenario**: User tells local opencode to review Go code, and a cloud opencode specializing in Go review is available.

```
1. User → Local Opencode: "Review the Go code in /src/server and fix any bugs"

2. Local Opencode's AI model recognizes it could benefit from a specialized reviewer
   → Calls discover_agents({ skill: "code-review", tags: ["go"], status: "available" })

3. Server queries agent_cards for workspace, returns:
   [
     { agent_id: "cloud-go-reviewer", name: "Go Reviewer", status: "available",
       skills: [{ name: "code-review", tags: ["go", "security", "performance"] }] },
     { agent_id: "cloud-nanoclaw", name: "Bug Hunter", status: "available",
       skills: [{ name: "bug-detection", tags: ["go", "python"] }] }
   ]

4. AI model decides "Go Reviewer" is the best match
   → Surfaces to user: "I found a Go Reviewer agent that specializes in Go code review.
      Shall I delegate the review to it?"
   → User: "Yes, go ahead"

5. AI calls delegate_task({
     target_id: "cloud-go-reviewer",
     skill: "code-review",
     input: {
       data_access: "callback",           // target will read files via MCP proxy
       path: "/src/server",
       focus: "bugs and security issues"
     },
     mode: "async"
   })

6. Server creates task (status: pending, delegation_chain: ["local-opencode"]),
   forwards to Go Reviewer via HTTP
   → Go Reviewer accepts (status: accepted → running)
   → Go Reviewer's task executor reads files from Local Opencode via MCP proxy:
     calls agent/{local-opencode-id}/read_file → server proxies via WS tunnel
   → Go Reviewer performs code review on the fetched files
   → Go Reviewer completes (status: completed, output: { findings: [...] })

7. Local Opencode polls with check_task({ task_id: "task-123" })
   → Gets review results with specific findings

8. AI presents findings to user:
   "The Go Reviewer found 3 issues:
    1. Potential nil pointer dereference in handler.go:45
    2. Missing error check on db.Query() in repo.go:112
    3. Race condition in cache.go:78
    Want me to fix these?"

9. User: "Yes, fix them all"
   → Local Opencode fixes the bugs using its own coding tools
```

---

## Database Migration

```sql
-- Migration: XXX_multi_agent_discovery.sql
-- Note: Uses TIMESTAMPTZ consistently to match existing schema conventions

-- Agent capability cards
CREATE TABLE agent_cards (
    sandbox_id TEXT PRIMARY KEY REFERENCES sandboxes(id) ON DELETE CASCADE,
    agent_type TEXT NOT NULL,
    agent_status TEXT NOT NULL DEFAULT 'available',
    card_json TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agent_cards_type ON agent_cards(agent_type);
CREATE INDEX idx_agent_cards_status ON agent_cards(agent_status);

-- Inter-agent tasks
CREATE TABLE agent_tasks (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    requester_id TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    skill TEXT,
    input_json TEXT NOT NULL,
    output_json TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    mode TEXT NOT NULL DEFAULT 'async',
    failure_reason TEXT,
    timeout_seconds INTEGER DEFAULT 300,
    delegation_chain TEXT NOT NULL DEFAULT '[]',  -- JSON array of sandbox IDs for loop detection
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accepted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_tasks_workspace ON agent_tasks(workspace_id);
CREATE INDEX idx_agent_tasks_requester ON agent_tasks(requester_id);
CREATE INDEX idx_agent_tasks_target_status ON agent_tasks(target_id, status);
CREATE INDEX idx_agent_tasks_cleanup ON agent_tasks(status, completed_at);  -- for TTL cleanup

-- Audit log for inter-agent interactions
-- Intentionally no FK constraints: audit records survive entity deletion
CREATE TABLE agent_interactions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    requester_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    interaction_type TEXT NOT NULL,
    detail_json TEXT,
    status TEXT NOT NULL,
    duration_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agent_interactions_workspace_time ON agent_interactions(workspace_id, created_at);

-- Workspace delegation mode setting
ALTER TABLE workspaces ADD COLUMN delegation_mode TEXT NOT NULL DEFAULT 'auto';
```

---

## Implementation Phases

### Phase 1: Foundation (Agent Cards + Discovery)
- `AgentAuthMiddleware` for proxy_token-based `/api/agent/` route group
- Database migration for `agent_cards` table (with indexed type/status columns)
- Agent Card Go types and validation
- Card registration endpoint (`POST /api/agent/discovery/cards`)
- Card discovery endpoint (`GET /api/agent/discovery/agents` with filters and pagination)
- Default card generation per agent type
- Extend heartbeat to carry card version + status
- `AgentHealthMonitor` background goroutine for offline detection

### Phase 2: Task Delegation
- Database migration for `agent_tasks` table
- Task creation endpoint (`POST /api/agent/tasks`) with server-side agent matching
- Task delivery to cloud agents (HTTP `POST /agent/tasks`) and local agents (tunnel `task_request`/`task_response` frames)
- Task executor infrastructure per agent type (opencode, openclaw, nanoclaw)
- Task status tracking and result storage
- Sync mode (blocking with timeout) and async mode
- Timeout handling and offline failure recovery
- **Rate limiting**: Per-agent rate limits on task creation (default: 10 tasks/min) and per-workspace aggregate limits (default: 50 tasks/min) to prevent delegation loops

### Phase 3: MCP Integration
- **Agent-side MCP server**: Build `POST /mcp/tools/call` endpoint for each agent type (opencode, openclaw, nanoclaw)
- MCP proxy in the server (parse `agent/{id}/tool` namespaced tool calls, forward to target)
- Tool injection into agent configs (opencode env var, openclaw gateway config, nanoclaw env vars)
- `inject_tools` tunnel frame type for local agents (with agent-side handler in `internal/agent/client.go`)
- `discover_agents`, `delegate_task`, `check_task` MCP tool implementations
- Tool namespacing logic (`agent/{sandbox_id}/tool_name`)

### Phase 4: Direct Fast-Path & Security
- JWT issuance (HMAC-SHA256 with `AGENTSERVER_JWT_SECRET`) for direct K8s pod-to-pod MCP calls
- JWT validation on agent-side MCP endpoints
- Audit logging table and logging middleware
- Workspace delegation mode (auto/approval) with UI for approval queue

### Phase 5: Observability & Polish
- Dashboard UI for viewing workspace agents and their capabilities
- Task history and interaction audit viewer
- Metrics and alerting (task latency, delegation success rate, agent availability)
- Performance optimization based on production data
