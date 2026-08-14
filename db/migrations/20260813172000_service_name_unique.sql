-- migrate:up

-- The floor: a service account's name is unique, the same as the other two
-- principal-kind identifiers (`human_username_key`, `node_name_key`).
--
-- #563 asked the question explicitly rather than inheriting the answer. It is
-- unique because the string is the only handle the row has, and because it is
-- DENORMALIZED as bare text where nothing else survives beside it: into
-- `audit_log.actor_username` at write time, so the trail still names its actor
-- after a purge, and into an alarm's acknowledgement on every read. Two service
-- accounts sharing a name make both of those unresolvable after the fact, which
-- is exactly the reason `human.username` is unique. An identifier that does not
-- identify is not one.
--
-- Free today and expensive later: there is no create path for a service
-- principal on the gateway or the API, so nothing can be holding a duplicate
-- that an operator typed, and the estate has no releases and no operator data
-- (the same ground #613 rests its nullability normalization on). The dedupe
-- backfill runs first regardless, because a migration that assumes an empty
-- table is a migration that fails on the one database that is not.
--
-- Case-sensitive, matching `human.username`: the case rule for an identifier is
-- one question for all three kinds and not a thing to settle differently here.
DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conrelid = 'service'::regclass AND conname = 'service_name_key'
  ) THEN
    ALTER TABLE service ADD CONSTRAINT service_name_key UNIQUE (name);
  END IF;
END $$;

-- migrate:down

ALTER TABLE service DROP CONSTRAINT IF EXISTS service_name_key;
