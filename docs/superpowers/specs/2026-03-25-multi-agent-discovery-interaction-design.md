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
// AgentCard represents an agent's capabilities and metadata
type AgentCard struct {
    // Identity
    AgentID     string    `json:"agent_id"`       // sandbox ID
    Name        string    `json:"name"`            // human-readable name
    Type        string    `json:"type"`            // "opencode" | "openclaw" | "nanoclaw"
    Description string    `json:"description"`     // what this agent does

    // Capabilities
    Skills      []Skill   `json:"skills"`          // high-level skill descriptions
    MCPTools    []MCPTool `json:"mcp_tools"`       // MCP tools this agent exposes

    // Availability
    Status      string    `json:"status"`          // "available" | "busy" | "offline"
    IsLocal     bool      `json:"is_local"`        // local vs cloud agent
    LastSeenAt  time.Time `json:"last_seen_at"`    // last heartbeat

    // Interaction
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
    card_json  TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Registration flow:**
1. Agent starts up and determines its capabilities (type defaults + custom config)
2. Agent reports its card to the server:
   - Cloud agents: POST to server endpoint during startup
   - Local agents: Send card via WebSocket tunnel alongside `agent_info`
3. Server stores the card in `agent_cards` table
4. On capability change, agent re-reports with incremented version
5. Server marks agents as `offline` when heartbeat lapses (60s threshold)

**Default cards by type:**
- The server pre-populates default skills and tools based on agent type
- Agents can extend or override defaults
- This means even agents that don't explicitly register a card will have basic discoverability

### 3. Discovery API

**Endpoints:**

```
GET  /api/workspaces/{wid}/agent-cards
     Query params:
       ?type=opencode          — filter by agent type
       ?status=available       — filter by availability
       ?skill=code-review      — filter by skill name
       ?tag=go                 — filter by skill tag
     Response: {
       "agents": [AgentCard, ...]
     }

GET  /api/workspaces/{wid}/agent-cards/{sandbox_id}
     Response: AgentCard (full, including MCP tool schemas)

POST /api/workspaces/{wid}/agent-cards/{sandbox_id}
     Auth: sandbox's proxy_token
     Body: AgentCard
     Response: { "version": N }
```

**Capability matching algorithm:**

When an agent queries for a capability, the server returns matching agents ranked by:

1. **Availability** (weight: critical) — `available` > `busy` with remaining capacity > `offline` (excluded)
2. **Skill match** (weight: high) — exact skill name match > tag overlap > type-inferred match
3. **Locality** (weight: medium) — prefer cloud↔cloud or local↔local for lower latency
4. **Load** (weight: low) — fewer active tasks preferred

**Injected MCP tool:**

```json
{
  "name": "discover_agents",
  "description": "Discover other agents in this workspace by skill, tags, or type. Returns available agents with their capabilities.",
  "input_schema": {
    "type": "object",
    "properties": {
      "skill": { "type": "string", "description": "Skill name to search for (e.g., 'code-review')" },
      "tags": { "type": "array", "items": { "type": "string" }, "description": "Tags to match (e.g., ['go', 'python'])" },
      "type": { "type": "string", "enum": ["opencode", "openclaw", "nanoclaw"], "description": "Filter by agent type" },
      "status": { "type": "string", "enum": ["available", "busy"], "description": "Filter by availability status" }
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
    requester_id TEXT NOT NULL REFERENCES sandboxes(id),
    target_id TEXT NOT NULL REFERENCES sandboxes(id),
    skill TEXT,                          -- requested skill
    input_json TEXT NOT NULL,            -- task input payload
    output_json TEXT,                    -- task result
    status TEXT NOT NULL DEFAULT 'pending',
    mode TEXT NOT NULL DEFAULT 'async',  -- "sync" | "async"
    failure_reason TEXT,                 -- reason for failure/rejection
    timeout_seconds INTEGER DEFAULT 300,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accepted_at TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX idx_agent_tasks_workspace ON agent_tasks(workspace_id);
CREATE INDEX idx_agent_tasks_requester ON agent_tasks(requester_id);
CREATE INDEX idx_agent_tasks_target ON agent_tasks(target_id, status);
```

**Delegation endpoint:**

```
POST /api/workspaces/{wid}/tasks
     Auth: requester's proxy_token
     Body: {
       "requester_id": "sandbox-a-id",
       "target_id": "sandbox-b-id",         // specific target, OR
       "target_skill": "code-review",        // let server pick best match
       "input": { ... },                     // task payload
       "mode": "sync" | "async",
       "timeout": 300
     }
     Response (async): { "task_id": "...", "status": "pending" }
     Response (sync):  { "task_id": "...", "status": "completed", "output": {...} }

GET  /api/workspaces/{wid}/tasks/{task_id}
     Response: full task record

GET  /api/workspaces/{wid}/tasks?requester={id}&status=running
     Response: { "tasks": [...] }
```

**Task delivery to target agent:**
- Cloud agents: HTTP POST to pod IP
- Local agents: Forward via WebSocket tunnel as a new frame type `task_request`

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

**Tool namespacing:**

When agent A discovers agent B's MCP tools, the tools are presented with a namespace prefix to prevent collisions:

```
agent-{agent_name}::tool_name
```

Example: If "Go Reviewer" agent has a tool called `analyze_code`, agent A sees it as `agent-go-reviewer::analyze_code`.

**MCP proxy flow:**

```
Agent A calls tool "agent-go-reviewer::analyze_code"
  → Server parses prefix, identifies target agent "Go Reviewer"
  → Server validates: both agents in same workspace? requester authorized?
  → Server forwards tool call to target agent:
      - Local agents: via WebSocket tunnel
      - Cloud agents: via HTTP to pod IP
  → Target agent executes the tool
  → Server relays result back to Agent A
  → Server logs the interaction to audit table
```

**Direct fast-path (K8s only):**

For cloud-to-cloud MCP tool calls within the same K8s namespace:

1. Agent A calls a namespaced MCP tool
2. Server checks: both agents are cloud, same namespace
3. Server issues a short-lived JWT (5min TTL):
   ```json
   {
     "requester_id": "sandbox-a",
     "target_id": "sandbox-b",
     "workspace_id": "ws-123",
     "allowed_tools": ["analyze_code", "run_tests"],
     "exp": 1711382400
   }
   ```
4. Server returns target's pod IP + port + JWT to Agent A
5. Agent A calls target directly at `pod-ip:mcp-port` with JWT in Authorization header
6. Server logs interaction metadata (but doesn't proxy the payload)

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

**Offline handling:**
1. Server marks agent as `offline` in the card
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
CREATE TABLE agent_interactions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    requester_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    interaction_type TEXT NOT NULL,  -- "discovery" | "task_create" | "task_complete" | "mcp_tool_call"
    detail_json TEXT,               -- request metadata (not full payloads)
    status TEXT NOT NULL,           -- "success" | "failed" | "rejected"
    duration_ms INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agent_interactions_workspace ON agent_interactions(workspace_id, created_at);
```

### 8. Agent-Type Integration

**Injected tools per agent type:**

All agent types receive three injected MCP tools: `discover_agents`, `delegate_task`, `check_task`. These are injected via the agent's configuration mechanism:

| Agent Type | Injection Method | How It Works |
|------------|-----------------|--------------|
| **opencode** | `OPENCODE_CONFIG_CONTENT` env var | Add tools to the MCP tools section of the opencode config JSON |
| **openclaw** | `openclaw.json` gateway config | Add as a built-in plugin/channel in the gateway configuration |
| **nanoclaw** | Environment variables | Set `AGENTSERVER_TASK_API_URL` and `AGENTSERVER_TASK_API_TOKEN` for the bridge to call |
| **local agents** | WebSocket tunnel messages | Server sends tool definitions as a new `inject_tools` tunnel frame type |

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
     input: { path: "/src/server", focus: "bugs and security issues" },
     mode: "async"
   })

6. Server creates task (status: pending), forwards to Go Reviewer via HTTP
   → Go Reviewer accepts (status: accepted → running)
   → Go Reviewer performs code review
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

-- Agent capability cards
CREATE TABLE agent_cards (
    sandbox_id TEXT PRIMARY KEY REFERENCES sandboxes(id) ON DELETE CASCADE,
    card_json TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Inter-agent tasks
CREATE TABLE agent_tasks (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    requester_id TEXT NOT NULL REFERENCES sandboxes(id),
    target_id TEXT NOT NULL REFERENCES sandboxes(id),
    skill TEXT,
    input_json TEXT NOT NULL,
    output_json TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    mode TEXT NOT NULL DEFAULT 'async',
    failure_reason TEXT,
    timeout_seconds INTEGER DEFAULT 300,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accepted_at TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE INDEX idx_agent_tasks_workspace ON agent_tasks(workspace_id);
CREATE INDEX idx_agent_tasks_requester ON agent_tasks(requester_id);
CREATE INDEX idx_agent_tasks_target_status ON agent_tasks(target_id, status);

-- Audit log for inter-agent interactions
CREATE TABLE agent_interactions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    requester_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    interaction_type TEXT NOT NULL,
    detail_json TEXT,
    status TEXT NOT NULL,
    duration_ms INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_agent_interactions_workspace_time ON agent_interactions(workspace_id, created_at);

-- Workspace delegation mode setting
ALTER TABLE workspaces ADD COLUMN delegation_mode TEXT NOT NULL DEFAULT 'auto';
```

---

## Implementation Phases

### Phase 1: Foundation (Agent Cards + Discovery)
- Database migration for `agent_cards` table
- Agent Card Go types and validation
- Card registration endpoint (POST)
- Card discovery endpoint (GET with filters)
- Default card generation per agent type
- Extend heartbeat to carry card version + status

### Phase 2: Task Delegation
- Database migration for `agent_tasks` table
- Task creation endpoint with server-side agent matching
- Task delivery to cloud agents (HTTP) and local agents (tunnel)
- Task status tracking and result storage
- Sync and async delegation modes
- Timeout handling

### Phase 3: MCP Integration
- MCP proxy in the server (parse namespaced tool calls, forward to target)
- Tool injection into agent configs (opencode, openclaw, nanoclaw)
- `discover_agents`, `delegate_task`, `check_task` tool implementations
- Tool namespacing logic

### Phase 4: Direct Fast-Path & Security
- JWT issuance for direct K8s pod-to-pod MCP calls
- MCP endpoint on each cloud agent for direct calls
- Audit logging table and logging middleware
- Workspace delegation mode (auto/approval)

### Phase 5: Polish & Observability
- Dashboard UI for viewing workspace agents and their capabilities
- Task history and interaction audit viewer
- Offline detection and graceful degradation
- Rate limiting on inter-agent interactions
- Metrics and alerting
