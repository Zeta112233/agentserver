-- Add bridge-mode credential columns to sandbox_weixin_bindings.
-- Used by nanoclaw sandboxes where agentserver bridges messages.
ALTER TABLE sandbox_weixin_bindings ADD COLUMN bot_token TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN ilink_base_url TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN webhook_registered BOOLEAN DEFAULT FALSE;

-- Index for reverse lookup: given a bot_id from iLink webhook, find the sandbox.
CREATE INDEX IF NOT EXISTS idx_weixin_bindings_bot_id ON sandbox_weixin_bindings(bot_id);

-- Polling state for long-poll bridge mode.
ALTER TABLE sandbox_weixin_bindings ADD COLUMN get_updates_buf TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN last_poll_at TIMESTAMPTZ;

-- Context token store: iLink requires echoing context_token on every outbound message.
CREATE TABLE IF NOT EXISTS weixin_context_tokens (
    sandbox_id TEXT NOT NULL REFERENCES sandboxes(id) ON DELETE CASCADE,
    bot_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    context_token TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (sandbox_id, bot_id, user_id)
);
