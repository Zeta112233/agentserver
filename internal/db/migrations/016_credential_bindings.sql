CREATE TABLE credential_bindings (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    server_url    TEXT NOT NULL,
    public_meta   JSONB NOT NULL DEFAULT '{}',
    auth_type     TEXT NOT NULL,
    auth_blob     BYTEA NOT NULL,
    is_default    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, kind, display_name)
);

CREATE INDEX idx_credential_bindings_ws_kind
    ON credential_bindings(workspace_id, kind);

-- Exactly one default per (workspace, kind).
CREATE UNIQUE INDEX idx_credential_bindings_one_default_per_kind
    ON credential_bindings(workspace_id, kind)
    WHERE is_default;
