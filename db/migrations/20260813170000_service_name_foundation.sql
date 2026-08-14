-- migrate:up

-- #563: service.label becomes service.name.
--
-- `service` has exactly two columns, `principal_id` and this one, and this one
-- is what IDENTIFIES a service principal: it is the username analogue for
-- kind=service, the only operator-visible handle the row has. Under the identity
-- triad (id / name / display_name) an identifier is a `name`, and the column was
-- the only place in the schema where `label` meant one.
--
-- Two more migrations follow rather than one: the duplicate names an existing
-- estate might hold are DATA, so disambiguating them is its own backfill
-- (20260813171000_service_name_dedupe.sql) before the uniqueness floor
-- (20260813172000_service_name_unique.sql), the same three-step shape the
-- assignment position took.
--
-- Postgres has no RENAME COLUMN IF EXISTS, so the rename is catalog-guarded and
-- the guard is mirrored in the down.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'service' AND column_name = 'label')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'service' AND column_name = 'name') THEN
    ALTER TABLE service RENAME COLUMN label TO name;
  END IF;
END $$;

-- migrate:down

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'service' AND column_name = 'name')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'service' AND column_name = 'label') THEN
    ALTER TABLE service RENAME COLUMN name TO label;
  END IF;
END $$;
