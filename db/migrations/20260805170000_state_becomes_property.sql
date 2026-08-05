-- migrate:up

-- #588: the `state` table becomes `property`. The record takes the bare noun
-- and the catalog keeps the _type suffix, matching event_type/event; the FK is
-- already property_type_id after the July rename, so it carries through. The
-- word "state" was overloaded (a health verdict is a state, a reachability
-- verdict is a state, and neither is this table); the lane is simply a value
-- over time, and its name now says so.
--
-- The two value columns also converge here: the text projection (`value`) and
-- the exact-jsonb sidecar (`value_json`, load-bearing since #591: writers carry
-- the jsonb beside the text, and the declared tombstone is JSON null) become
-- one `value jsonb NOT NULL`. coalesce(value_json, to_jsonb(value)) preserves
-- every row: a structured value keeps its exact jsonb, a text-only row encodes
-- as a jsonb string, and the tombstones keep their JSON null.

-- The name must be free. #591 dropped the value store to free it; a `property`
-- table still standing beside `state` means that fold never ran, and renaming
-- over it would silently merge two different tables. Refuse loudly instead.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property')
     AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'state') THEN
    RAISE EXCEPTION 'a table named property already exists beside state: the #591 value-store fold never ran, refusing to rename over it';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'state')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property') THEN
    ALTER TABLE state RENAME TO property;
  END IF;
END $$;

-- Every dependent identifier follows the table: state_* -> property_*. The
-- inventory is enumerated from the live schema (init plus the July rename that
-- made the FK property_type_id), not guessed. Renaming the PK constraint also
-- renames its backing index, so it is not listed again under the indexes.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property', 'state_pkey', 'property_pkey'),
    ('property', 'state_lineage_check', 'property_lineage_check'),
    ('property', 'state_owner_arc_check', 'property_owner_arc_check'),
    ('property', 'state_owner_kind_check', 'property_owner_kind_check'),
    ('property', 'state_provenance_check', 'property_provenance_check'),
    ('property', 'state_component_id_fkey', 'property_component_id_fkey'),
    ('property', 'state_event_id_fkey', 'property_event_id_fkey'),
    ('property', 'state_location_id_fkey', 'property_location_id_fkey'),
    ('property', 'state_node_id_fkey', 'property_node_id_fkey'),
    ('property', 'state_property_type_id_fkey', 'property_property_type_id_fkey'),
    ('property', 'state_system_id_fkey', 'property_system_id_fkey'),
    ('property', 'state_id_not_null', 'property_id_not_null'),
    ('property', 'state_ts_not_null', 'property_ts_not_null'),
    ('property', 'state_owner_kind_not_null', 'property_owner_kind_not_null'),
    ('property', 'state_instance_not_null', 'property_instance_not_null'),
    -- The text value's NOT NULL: renamed with the rest, then dropped with its
    -- column below, which frees the name for the converged jsonb column.
    ('property', 'state_value_not_null', 'property_value_not_null'),
    ('property', 'state_provenance_not_null', 'property_provenance_not_null'),
    ('property', 'state_source_not_null', 'property_source_not_null'),
    ('property', 'state_property_type_id_not_null', 'property_property_type_id_not_null')
  ) AS t(tbl, oldname, newname) LOOP
    IF to_regclass(r.tbl) IS NOT NULL
       AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.newname) THEN
      EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- Plain (non-constraint) indexes.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('state_owner_idx', 'property_owner_idx'),
    ('state_ts_brin', 'property_ts_brin')
  ) AS t(oldname, newname) LOOP
    IF EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.newname) THEN
      EXECUTE format('ALTER INDEX %I RENAME TO %I', r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- The identity sequence.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'S' AND relname = 'state_id_seq')
     AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'S' AND relname = 'property_id_seq') THEN
    ALTER SEQUENCE state_id_seq RENAME TO property_id_seq;
  END IF;
END $$;

-- The value convergence. Guarded on value_json still existing so a re-run
-- (which finds the converted single column) skips the whole block. SET NOT
-- NULL runs before the drops so a hole in the backfill would raise while both
-- source columns still exist.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'property' AND column_name = 'value_json') THEN
    ALTER TABLE property ADD COLUMN IF NOT EXISTS value_new jsonb;
    UPDATE property SET value_new = coalesce(value_json, to_jsonb(value)) WHERE value_new IS NULL;
    ALTER TABLE property ALTER COLUMN value_new SET NOT NULL;
    ALTER TABLE property DROP COLUMN value;
    ALTER TABLE property DROP COLUMN value_json;
    ALTER TABLE property RENAME COLUMN value_new TO value;
  END IF;
END $$;

-- SET NOT NULL auto-named its constraint after the scratch column; give it the
-- converged column's name (the drop above freed it).
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'property'::regclass AND conname = 'property_value_new_not_null')
     AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'property'::regclass AND conname = 'property_value_not_null') THEN
    ALTER TABLE property RENAME CONSTRAINT property_value_new_not_null TO property_value_not_null;
  END IF;
END $$;

-- migrate:down

-- The reverse, in reverse order: split the one jsonb value back into the text
-- projection plus the value_json sidecar, return every dependent identifier,
-- then give the table its old name. The split mirrors the old writers exactly:
-- value = coalesce(value #>> '{}', '') (an object serializes, a JSON-null
-- tombstone projects to ''), value_json = the exact jsonb only where the text
-- projection alone could not reproduce it (non-string types, including the
-- tombstone's JSON null). A jsonb string re-derives from the text on the next
-- up (to_jsonb of text), so up-down-up is stable for every row.

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                     WHERE table_name = 'property' AND column_name = 'value_json') THEN
    ALTER TABLE property ADD COLUMN IF NOT EXISTS value_text text;
    UPDATE property SET value_text = coalesce(value #>> '{}', '');
    ALTER TABLE property ADD COLUMN IF NOT EXISTS value_json jsonb;
    UPDATE property SET value_json = CASE WHEN jsonb_typeof(value) = 'string' THEN NULL ELSE value END;
    ALTER TABLE property ALTER COLUMN value_text SET NOT NULL;
    ALTER TABLE property DROP COLUMN value;
    ALTER TABLE property RENAME COLUMN value_text TO value;
  END IF;
END $$;

-- SET NOT NULL auto-named its constraint after the scratch column; the drop of
-- the jsonb column freed the settled name, so reclaim it before the bulk
-- rename below carries it back to state_value_not_null.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'property'::regclass AND conname = 'property_value_text_not_null')
     AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'property'::regclass AND conname = 'property_value_not_null') THEN
    ALTER TABLE property RENAME CONSTRAINT property_value_text_not_null TO property_value_not_null;
  END IF;
END $$;

-- Dependent identifiers back: property_* -> state_*.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property', 'property_pkey', 'state_pkey'),
    ('property', 'property_lineage_check', 'state_lineage_check'),
    ('property', 'property_owner_arc_check', 'state_owner_arc_check'),
    ('property', 'property_owner_kind_check', 'state_owner_kind_check'),
    ('property', 'property_provenance_check', 'state_provenance_check'),
    ('property', 'property_component_id_fkey', 'state_component_id_fkey'),
    ('property', 'property_event_id_fkey', 'state_event_id_fkey'),
    ('property', 'property_location_id_fkey', 'state_location_id_fkey'),
    ('property', 'property_node_id_fkey', 'state_node_id_fkey'),
    ('property', 'property_property_type_id_fkey', 'state_property_type_id_fkey'),
    ('property', 'property_system_id_fkey', 'state_system_id_fkey'),
    ('property', 'property_id_not_null', 'state_id_not_null'),
    ('property', 'property_ts_not_null', 'state_ts_not_null'),
    ('property', 'property_owner_kind_not_null', 'state_owner_kind_not_null'),
    ('property', 'property_instance_not_null', 'state_instance_not_null'),
    ('property', 'property_value_not_null', 'state_value_not_null'),
    ('property', 'property_provenance_not_null', 'state_provenance_not_null'),
    ('property', 'property_source_not_null', 'state_source_not_null'),
    ('property', 'property_property_type_id_not_null', 'state_property_type_id_not_null')
  ) AS t(tbl, oldname, newname) LOOP
    IF to_regclass(r.tbl) IS NOT NULL
       AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.newname) THEN
      EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property_owner_idx', 'state_owner_idx'),
    ('property_ts_brin', 'state_ts_brin')
  ) AS t(oldname, newname) LOOP
    IF EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.newname) THEN
      EXECUTE format('ALTER INDEX %I RENAME TO %I', r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'S' AND relname = 'property_id_seq')
     AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'S' AND relname = 'state_id_seq') THEN
    ALTER SEQUENCE property_id_seq RENAME TO state_id_seq;
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'state') THEN
    ALTER TABLE property RENAME TO state;
  END IF;
END $$;
