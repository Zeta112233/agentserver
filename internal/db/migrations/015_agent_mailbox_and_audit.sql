-- Agent mailbox: async message passing between agents
CREATE TABLE agent_mailbox (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    from_id       TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    to_id         TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    text          TEXT NOT NULL,
    msg_type      TEXT NOT NULL DEFAULT 'text',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at       TIMESTAMPTZ
);
CREATE INDEX idx_agent_mailbox_to ON agent_mailbox(to_id, read_at);
CREATE INDEX idx_agent_mailbox_workspace ON agent_mailbox(workspace_id);

-- Agent interactions: audit trail for multi-agent activity
CREATE TABLE agent_interactions (
    id              BIGSERIAL PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_id        TEXT REFERENCES sandboxes(id) ON DELETE SET NULL,
    action          TEXT NOT NULL,
    target_id       TEXT,
    target_type     TEXT NOT NULL DEFAULT 'task',
    detail_json     JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_interactions_workspace ON agent_interactions(workspace_id, created_at DESC);

-- Workspace delegation mode
ALTER TABLE workspaces ADD COLUMN delegation_mode TEXT NOT NULL DEFAULT 'auto';
