-- migrate:up

-- A Term receipt (#1691): the receipt of a batch the server Termed whole (an
-- owner asserted on the node lane, task rows outside its groups, a batch the
-- gateway refused, unreadable delivery metadata, or the last delivery the
-- consumer allows), carrying its ledger and every declared item under that
-- cause. termed marks it so it never reads as landed: a later copy of the
-- batch that lands replaces it, and the receipt count (#1639) skips it. A
-- receipt written before this landed its batch, so it reads not termed.
ALTER TABLE telemetry_receipt ADD COLUMN IF NOT EXISTS termed boolean NOT NULL DEFAULT false;

-- migrate:down
ALTER TABLE telemetry_receipt DROP COLUMN IF EXISTS termed;
