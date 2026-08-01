-- migrate:up
-- #465 / ADR-0075: an alarm gains its condition identity. dedup_key names
-- WHICH condition is open (the raiser supplies it; the future rule engine's
-- event_rule_id joins alongside it with that slice), and the partial unique
-- index makes "one open alarm per condition per component" a database fact
-- while leaving multi-condition components intact. Existing open rows predate
-- condition identity, so each is backfilled with its own row id as a unique
-- placeholder key (a data backfill required for the NOT NULL + unique pair;
-- their real condition was never recorded).
ALTER TABLE alarm ADD COLUMN IF NOT EXISTS dedup_key text;
UPDATE alarm SET dedup_key = id::text WHERE dedup_key IS NULL;
ALTER TABLE alarm ALTER COLUMN dedup_key SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS alarm_open_condition_key
    ON alarm (component_id, dedup_key) WHERE cleared_at IS NULL;

-- migrate:down
DROP INDEX IF EXISTS alarm_open_condition_key;
ALTER TABLE alarm DROP COLUMN IF EXISTS dedup_key;
