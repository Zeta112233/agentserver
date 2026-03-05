-- Per-workspace quota overrides for the LLM proxy.
CREATE TABLE workspace_quotas (
    workspace_id TEXT PRIMARY KEY,
    max_rpd      INTEGER,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
