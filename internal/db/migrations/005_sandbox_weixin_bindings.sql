CREATE TABLE sandbox_weixin_bindings (
    id            SERIAL PRIMARY KEY,
    sandbox_id    TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    bot_id        TEXT NOT NULL,
    user_id       TEXT NOT NULL DEFAULT '',
    bound_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sandbox_weixin_bindings_sandbox_id ON sandbox_weixin_bindings(sandbox_id);
