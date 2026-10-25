-- migrate:up

-- The channel an audited action arrived through (#1162, ADR-1162). An MCP tool
-- call runs the same route as a direct API call and audits as the same actor;
-- channel says it came over MCP, and client is the name and version the MCP
-- client declared about itself, recorded as asserted. Both are null for a
-- direct API call. The server stamps them on the request context, so no
-- caller can claim a channel; the check holds the column to the channels the
-- server knows ('omnibot' is reserved for the in-app assistant, #1166, the way
-- principal.kind reserves 'agent').
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS channel text;
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS client text;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'audit_log_channel_check') THEN
    ALTER TABLE audit_log ADD CONSTRAINT audit_log_channel_check
      CHECK (channel IS NULL OR channel IN ('mcp', 'omnibot'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'audit_log_client_length_check') THEN
    ALTER TABLE audit_log ADD CONSTRAINT audit_log_client_length_check
      CHECK (client IS NULL OR char_length(client) <= 200);
  END IF;
END $$;

-- migrate:down

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_client_length_check;
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_channel_check;
ALTER TABLE audit_log DROP COLUMN IF EXISTS client;
ALTER TABLE audit_log DROP COLUMN IF EXISTS channel;
