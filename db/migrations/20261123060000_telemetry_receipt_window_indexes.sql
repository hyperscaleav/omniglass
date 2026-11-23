-- migrate:up

-- The lab's delivery read (#1947) selects the boots that minted in a window by
-- minted, then each boot's receipts by (boot, seq), so it costs what the
-- window holds instead of window-sorting every receipt of the day. Partial on
-- the ledger: only a ledgered receipt is ever read.
CREATE INDEX IF NOT EXISTS telemetry_receipt_ledgered_minted_idx ON telemetry_receipt (minted) WHERE ledger IS NOT NULL;
CREATE INDEX IF NOT EXISTS telemetry_receipt_ledgered_boot_seq_idx ON telemetry_receipt (boot, seq) WHERE ledger IS NOT NULL;

-- migrate:down
DROP INDEX IF EXISTS telemetry_receipt_ledgered_boot_seq_idx;
DROP INDEX IF EXISTS telemetry_receipt_ledgered_minted_idx;
