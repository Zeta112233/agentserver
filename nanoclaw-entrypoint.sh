#!/bin/sh
# Write .env from NANOCLAW_CONFIG_CONTENT environment variable.
# Same pattern as openclaw config injection via shell heredoc.
if [ -n "$NANOCLAW_CONFIG_CONTENT" ]; then
    echo "$NANOCLAW_CONFIG_CONTENT" > /app/.env
fi
exec "$@"
