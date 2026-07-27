-- migrate:up
-- ADR-0063 #394: the property cache holds the value's own time (the sample time
-- for observed, the effect time for intended), distinct from created_at/updated_at
-- (row-write time). A latest-value cache and command settlement both key off the
-- value's own time. Additive and idempotent; no backfill (dev data re-derives).
alter table property add column if not exists ts timestamptz;

-- migrate:down
alter table property drop column if exists ts;
