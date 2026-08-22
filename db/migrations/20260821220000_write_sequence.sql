-- migrate:up

-- The DB-assigned write sequence (#780). Clock-derived keys (created_at from
-- now(), uuidv7 ids) are not a write order: on a VM whose wall clock steps
-- (WSL2 correcting multi-second NTP drift under load), stamps taken by
-- different transactions invert relative to true insertion order, and the
-- lists ordered by them (the audit trail, the users directory) flapped
-- between two reads of the same data. An identity column is allocated by the
-- database at insert, monotonic per table, and no clock touches it.
-- The backfill on an existing table assigns values in heap-scan order, which
-- for these append-only tables is insertion order.
ALTER TABLE principal ADD COLUMN IF NOT EXISTS seq bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS seq bigint GENERATED ALWAYS AS IDENTITY;

-- The audit trail reads newest-first with a limit on every page; the directory
-- is small, but the trail grows without bound and needs the ordered path.
CREATE INDEX IF NOT EXISTS audit_log_seq_idx ON audit_log (seq DESC);

-- migrate:down

DROP INDEX IF EXISTS audit_log_seq_idx;
ALTER TABLE audit_log DROP COLUMN IF EXISTS seq;
ALTER TABLE principal DROP COLUMN IF EXISTS seq;
