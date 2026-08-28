-- migrate:up

-- The command wire (#815): the execution arc beside the settlement arc. A
-- command is dispatched when a node's queue pull delivers it, executed when
-- the node reports the actuation (exec_error carries a failed one's story);
-- settlement stays the separate judgment of observed against intended.
ALTER TABLE command ADD COLUMN IF NOT EXISTS dispatched_at timestamptz;
ALTER TABLE command ADD COLUMN IF NOT EXISTS executed_at timestamptz;
ALTER TABLE command ADD COLUMN IF NOT EXISTS exec_error text;
-- The endpoint a dispatch chose to actuate over: the confinement anchor for the
-- execution report (only the node this endpoint is placed on may stamp it) and
-- the record of which of a component's endpoints carried the command.
ALTER TABLE command ADD COLUMN IF NOT EXISTS dispatched_endpoint_id uuid REFERENCES endpoint(id) ON DELETE SET NULL;

-- migrate:down

ALTER TABLE command DROP COLUMN IF EXISTS dispatched_endpoint_id;
ALTER TABLE command DROP COLUMN IF EXISTS exec_error;
ALTER TABLE command DROP COLUMN IF EXISTS executed_at;
ALTER TABLE command DROP COLUMN IF EXISTS dispatched_at;
