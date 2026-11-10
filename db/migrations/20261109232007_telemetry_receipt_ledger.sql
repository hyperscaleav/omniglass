-- migrate:up

-- The receipt as the batch's accounting row (#1641, ADR-1641): beside the
-- batch id, the node's delivery ledger the batch carried (its node and boot,
-- its dense seq in that boot, when the node minted it, the highest seq every
-- batch at or below which had left the node's spool, and its declared item
-- counts by kind), how many times it was delivered, and what became of each
-- of its items by kind and outcome. Written in the batch's own transaction,
-- so the accounting is exactly once with the data. Columns only, no new row
-- and no index: the lab's read scans at most a day of receipts once. A
-- receipt landed before this (or by the API lane, which carries no ledger)
-- reads one delivery and no ledger.
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS node text;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS boot text;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS seq bigint;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS minted timestamptz;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS resolved bigint;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS deliveries int NOT NULL DEFAULT 1;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS ledger jsonb;
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS outcome jsonb;

-- migrate:down
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS outcome;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS ledger;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS deliveries;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS resolved;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS minted;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS seq;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS boot;
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS node;
