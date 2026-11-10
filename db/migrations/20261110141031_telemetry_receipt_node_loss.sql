-- migrate:up

-- The node hop's own account on the receipt (#1692): the runs of seqs the
-- node's spool dropped (at a cap, aged out, refused by the bus) that no
-- stored batch had carried yet, and the items it lost before any batch
-- carried them, running totals for the boot by cause and kind. A read
-- names a gap in a boot's seqs as the node's loss when a receipt says the
-- node dropped it, and as the ingest's missing otherwise. Null when the
-- node had lost nothing, and on every receipt landed before this.
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS node_loss jsonb;

-- migrate:down
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS node_loss;
