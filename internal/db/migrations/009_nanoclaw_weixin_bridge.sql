-- Add bridge-mode credential columns to sandbox_weixin_bindings.
-- Used by nanoclaw sandboxes where agentserver bridges messages.
ALTER TABLE sandbox_weixin_bindings ADD COLUMN bot_token TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN ilink_base_url TEXT;
ALTER TABLE sandbox_weixin_bindings ADD COLUMN webhook_registered BOOLEAN DEFAULT FALSE;

-- Index for reverse lookup: given a bot_id from iLink webhook, find the sandbox.
CREATE INDEX IF NOT EXISTS idx_weixin_bindings_bot_id ON sandbox_weixin_bindings(bot_id);
