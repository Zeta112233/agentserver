-- agent_sessions: session lifecycle (aligned with CC /v1/code/sessions)
CREATE TABLE agent_sessions (
    id            TEXT PRIMARY KEY,
    sandbox_id    TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    epoch         INTEGER NOT NULL DEFAULT 0,
    tags          TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at   TIMESTAMPTZ
);
CREATE INDEX idx_agent_sessions_sandbox ON agent_sessions(sandbox_id);
CREATE INDEX idx_agent_sessions_workspace ON agent_sessions(workspace_id);

-- agent_session_events: append-only event log, BIGSERIAL = sequence_num
CREATE TABLE agent_session_events (
    id            BIGSERIAL PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    event_id      TEXT NOT NULL,
    event_type    TEXT NOT NULL DEFAULT 'client_event',
    source        TEXT NOT NULL DEFAULT 'client',
    epoch         INTEGER NOT NULL DEFAULT 0,
    payload       JSONB NOT NULL,
    ephemeral     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_agent_session_events_eid ON agent_session_events(event_id);
CREATE INDEX idx_agent_session_events_session_seq ON agent_session_events(session_id, id);

-- agent_session_workers: one active worker per epoch
CREATE TABLE agent_session_workers (
    session_id              TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    epoch                   INTEGER NOT NULL,
    state                   TEXT NOT NULL DEFAULT 'idle',
    external_metadata       JSONB,
    requires_action_details JSONB,
    last_heartbeat_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    registered_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, epoch)
);

-- agent_session_internal_events: transcript/compaction (not visible to clients)
CREATE TABLE agent_session_internal_events (
    id            BIGSERIAL PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL,
    payload       JSONB NOT NULL,
    is_compaction BOOLEAN NOT NULL DEFAULT FALSE,
    agent_id      TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_internal_events_session ON agent_session_internal_events(session_id, id);

-- agent_cards: agent capability declaration
CREATE TABLE agent_cards (
    sandbox_id    TEXT PRIMARY KEY REFERENCES sandboxes(id) ON DELETE CASCADE,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_type    TEXT NOT NULL DEFAULT 'claudecode',
    display_name  TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    card_json     JSONB NOT NULL DEFAULT '{}',
    agent_status  TEXT NOT NULL DEFAULT 'offline',
    version       INTEGER NOT NULL DEFAULT 1,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_cards_workspace ON agent_cards(workspace_id);
CREATE INDEX idx_agent_cards_status ON agent_cards(agent_status);

-- agent_tasks: task delegation
CREATE TABLE agent_tasks (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    requester_id    TEXT REFERENCES sandboxes(id) ON DELETE SET NULL,
    target_id       TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    session_id      TEXT REFERENCES agent_sessions(id) ON DELETE SET NULL,
    skill           TEXT,
    prompt          TEXT NOT NULL,
    system_context  TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    result_json     JSONB,
    failure_reason  TEXT,
    timeout_seconds INTEGER DEFAULT 300,
    delegation_chain TEXT[] NOT NULL DEFAULT '{}',
    total_cost_usd  REAL,
    num_turns       INTEGER DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at     TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ
);
CREATE INDEX idx_agent_tasks_workspace ON agent_tasks(workspace_id);
CREATE INDEX idx_agent_tasks_target ON agent_tasks(target_id, status);
