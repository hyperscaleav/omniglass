-- migrate:up

-- A standard the boot seed plants (SeedStandard: by name, only when absent) is
-- marked, so a rename can refuse it (#555): renaming one would have the next
-- boot plant the shipped name again beside the operator's renamed row, with the
-- shipped roles and choices hung on it. The seed sets the mark on every boot,
-- including on a row an older binary seeded, so no backfill runs here. Forking
-- a template into a standard of the operator's own is #827's.
ALTER TABLE standard ADD COLUMN IF NOT EXISTS seeded boolean NOT NULL DEFAULT false;

-- migrate:down

ALTER TABLE standard DROP COLUMN IF EXISTS seeded;
