-- migrate:up

-- Enrollment adds two columns to the collection tier's node table (cp1's
-- 20260707150000_collection.sql). Additive and idempotent; never edit the applied
-- cp1 migration. enrollment_token holds the hex sha256 of the node's enrollment
-- token (never the cleartext); enrolled_at is set when the node first claims its
-- identity in exchange for its NATS credential.
alter table node add column if not exists enrollment_token text;
alter table node add column if not exists enrolled_at      timestamptz;

-- migrate:down

alter table node drop column if exists enrolled_at;
alter table node drop column if exists enrollment_token;
