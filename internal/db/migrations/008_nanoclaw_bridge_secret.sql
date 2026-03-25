-- Add nanoclaw bridge secret column to sandboxes table.
-- Stores the shared secret for HTTP auth between agentserver and NanoClaw pod.
ALTER TABLE sandboxes ADD COLUMN nanoclaw_bridge_secret TEXT;
