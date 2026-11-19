-- migrate:up
-- Agent tokens (#1179): a token a person mints for an agent to act on their
-- behalf. A non-null credential.agent names the agent and marks the token as
-- one. agent_roles is the role set the agent may use, intersected with the
-- minting person's capability on every request (null: the person's own,
-- intersected all the same); agent_channels the entry points it is accepted on
-- ('api', 'mcp'; null: any). audit_log.agent records the agent a row was taken
-- by, on its person's behalf; null for a row a person took directly.
ALTER TABLE credential ADD COLUMN IF NOT EXISTS agent text;
ALTER TABLE credential ADD COLUMN IF NOT EXISTS agent_roles text[];
ALTER TABLE credential ADD COLUMN IF NOT EXISTS agent_channels text[];
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS agent text;
CREATE INDEX IF NOT EXISTS audit_log_agent_idx ON audit_log (agent, seq DESC) WHERE agent IS NOT NULL;

-- migrate:down
DROP INDEX IF EXISTS audit_log_agent_idx;
ALTER TABLE audit_log DROP COLUMN IF EXISTS agent;
ALTER TABLE credential DROP COLUMN IF EXISTS agent_channels;
ALTER TABLE credential DROP COLUMN IF EXISTS agent_roles;
ALTER TABLE credential DROP COLUMN IF EXISTS agent;
