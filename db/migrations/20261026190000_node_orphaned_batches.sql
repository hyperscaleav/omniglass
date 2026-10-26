-- migrate:up

-- A node batch the confinement fence orphans (a task on another node, an
-- unknown task) has its samples dropped by design (ADR-0037), and until #1200
-- the drop left no trace. These columns are the operator-visible count: how
-- many of this node's batches were orphaned, and when the newest was. The
-- server adds to them once per flush window, never once per batch.
ALTER TABLE node ADD COLUMN IF NOT EXISTS orphaned_batches bigint NOT NULL DEFAULT 0;
ALTER TABLE node ADD COLUMN IF NOT EXISTS last_orphaned_at timestamptz;

-- migrate:down

ALTER TABLE node DROP COLUMN IF EXISTS last_orphaned_at;
ALTER TABLE node DROP COLUMN IF EXISTS orphaned_batches;
