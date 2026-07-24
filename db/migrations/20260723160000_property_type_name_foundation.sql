-- migrate:up

-- ADR-0063: registries take the _type suffix, the bare noun holds the data.
-- The signal-definition registry becomes `property_type`; the latest-value
-- store (was `property_value`) takes the freed bare noun `property`. Every
-- data table's `property_id` FK references the registry, so it becomes
-- `property_type_id`. After the table and column renames, every dependent
-- object name (constraints and indexes: `property_pkey`, `property_value_series_key`,
-- `metric_property_id_fkey`, and the like) is brought in line with its table.

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property_type') THEN
    ALTER TABLE property RENAME TO property_type;
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property_value')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property') THEN
    ALTER TABLE property_value RENAME TO property;
  END IF;
END $$;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['metric', 'state', 'event', 'product_property', 'standard_property', 'location_type_property', 'property'] LOOP
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = t AND column_name = 'property_id')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = t AND column_name = 'property_type_id') THEN
      EXECUTE format('ALTER TABLE %I RENAME COLUMN property_id TO property_type_id', t);
    END IF;
  END LOOP;
END $$;

-- Bring dependent object names (constraints, indexes) in line with the renamed
-- tables and the property_type_id column. Registry BEFORE value store: renaming
-- property_pkey -> property_type_pkey frees the property_pkey name for the value
-- store's property_value_pkey to take. Renaming a PK/UNIQUE constraint also
-- renames its backing index, so those are not listed again below.

-- Registry (property_type, was property): property_* -> property_type_*.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property_type', 'property_pkey', 'property_type_pkey'),
    ('property_type', 'property_handle_key', 'property_type_handle_key'),
    ('property_type', 'property_data_type_check', 'property_type_data_type_check'),
    ('property_type', 'property_kind_check', 'property_type_kind_check'),
    ('property_type', 'property_description_not_null', 'property_type_description_not_null'),
    ('property_type', 'property_id_not_null', 'property_type_id_not_null'),
    ('property_type', 'property_name_not_null', 'property_type_name_not_null'),
    ('property_type', 'property_official_not_null', 'property_type_official_not_null'),
    ('property_type', 'property_registered_at_not_null', 'property_type_registered_at_not_null'),
    ('property_type', 'property_value_type_not_null', 'property_type_data_type_not_null')
  ) AS t(tbl, oldname, newname) LOOP
    IF to_regclass(r.tbl) IS NOT NULL
       AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.newname) THEN
      EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- Value store (property, was property_value): property_value_* -> property_*.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property', 'property_value_pkey', 'property_pkey'),
    ('property', 'property_value_series_key', 'property_series_key'),
    ('property', 'property_value_owner_arc_check', 'property_owner_arc_check'),
    ('property', 'property_value_owner_kind_check', 'property_owner_kind_check'),
    ('property', 'property_value_provenance_check', 'property_provenance_check'),
    ('property', 'property_value_component_id_fkey', 'property_component_id_fkey'),
    ('property', 'property_value_location_id_fkey', 'property_location_id_fkey'),
    ('property', 'property_value_node_id_fkey', 'property_node_id_fkey'),
    ('property', 'property_value_system_id_fkey', 'property_system_id_fkey'),
    ('property', 'property_value_property_id_fkey', 'property_property_type_id_fkey'),
    ('property', 'property_value_created_at_not_null', 'property_created_at_not_null'),
    ('property', 'property_value_id_not_null', 'property_id_not_null'),
    ('property', 'property_value_instance_not_null', 'property_instance_not_null'),
    ('property', 'property_value_owner_kind_not_null', 'property_owner_kind_not_null'),
    ('property', 'property_value_property_id_not_null', 'property_property_type_id_not_null'),
    ('property', 'property_value_provenance_not_null', 'property_provenance_not_null'),
    ('property', 'property_value_updated_at_not_null', 'property_updated_at_not_null'),
    ('property', 'property_value_value_not_null', 'property_value_not_null')
  ) AS t(tbl, oldname, newname) LOOP
    IF to_regclass(r.tbl) IS NOT NULL
       AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.newname) THEN
      EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- Data and classifier tables: the property_id FK objects -> property_type_id.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('metric', 'metric_property_id_fkey', 'metric_property_type_id_fkey'),
    ('metric', 'metric_property_id_not_null', 'metric_property_type_id_not_null'),
    ('state', 'state_property_id_fkey', 'state_property_type_id_fkey'),
    ('state', 'state_property_id_not_null', 'state_property_type_id_not_null'),
    ('event', 'event_property_id_fkey', 'event_property_type_id_fkey'),
    ('event', 'event_property_id_not_null', 'event_property_type_id_not_null'),
    ('product_property', 'product_property_property_id_fkey', 'product_property_property_type_id_fkey'),
    ('product_property', 'product_property_property_id_not_null', 'product_property_property_type_id_not_null'),
    ('product_property', 'product_property_product_id_property_id_key', 'product_property_product_id_property_type_id_key'),
    ('standard_property', 'standard_property_property_id_fkey', 'standard_property_property_type_id_fkey'),
    ('standard_property', 'standard_property_property_id_not_null', 'standard_property_property_type_id_not_null'),
    ('standard_property', 'standard_property_standard_id_property_id_key', 'standard_property_standard_id_property_type_id_key'),
    ('location_type_property', 'location_type_property_property_id_fkey', 'location_type_property_property_type_id_fkey'),
    ('location_type_property', 'location_type_property_property_id_not_null', 'location_type_property_property_type_id_not_null'),
    ('location_type_property', 'location_type_property_location_type_id_property_id_key', 'location_type_property_location_type_id_property_type_id_key')
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
    ('property_value_component_idx', 'property_component_idx'),
    ('product_property_property_idx', 'product_property_property_type_idx'),
    ('standard_property_property_idx', 'standard_property_property_type_idx'),
    ('location_type_property_property_idx', 'location_type_property_property_type_idx')
  ) AS t(oldname, newname) LOOP
    IF EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.newname) THEN
      EXECUTE format('ALTER INDEX %I RENAME TO %I', r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- migrate:down

-- Revert the column and table renames first, so the dependent-object renames
-- below run against the original table names (property = the registry,
-- property_value = the value store). Reverting first keeps this section re-runnable.

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['metric', 'state', 'event', 'product_property', 'standard_property', 'location_type_property', 'property'] LOOP
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = t AND column_name = 'property_type_id')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = t AND column_name = 'property_id') THEN
      EXECUTE format('ALTER TABLE %I RENAME COLUMN property_type_id TO property_id', t);
    END IF;
  END LOOP;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property_value') THEN
    ALTER TABLE property RENAME TO property_value;
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property_type')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property') THEN
    ALTER TABLE property_type RENAME TO property;
  END IF;
END $$;

-- Value store (property_value) BEFORE registry (property): renaming property_value's
-- property_pkey back frees the property_pkey name for the registry to reclaim.

-- Value store (property_value): property_* -> property_value_*.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property_value', 'property_pkey', 'property_value_pkey'),
    ('property_value', 'property_series_key', 'property_value_series_key'),
    ('property_value', 'property_owner_arc_check', 'property_value_owner_arc_check'),
    ('property_value', 'property_owner_kind_check', 'property_value_owner_kind_check'),
    ('property_value', 'property_provenance_check', 'property_value_provenance_check'),
    ('property_value', 'property_component_id_fkey', 'property_value_component_id_fkey'),
    ('property_value', 'property_location_id_fkey', 'property_value_location_id_fkey'),
    ('property_value', 'property_node_id_fkey', 'property_value_node_id_fkey'),
    ('property_value', 'property_system_id_fkey', 'property_value_system_id_fkey'),
    ('property_value', 'property_property_type_id_fkey', 'property_value_property_id_fkey'),
    ('property_value', 'property_created_at_not_null', 'property_value_created_at_not_null'),
    ('property_value', 'property_id_not_null', 'property_value_id_not_null'),
    ('property_value', 'property_instance_not_null', 'property_value_instance_not_null'),
    ('property_value', 'property_owner_kind_not_null', 'property_value_owner_kind_not_null'),
    ('property_value', 'property_property_type_id_not_null', 'property_value_property_id_not_null'),
    ('property_value', 'property_provenance_not_null', 'property_value_provenance_not_null'),
    ('property_value', 'property_updated_at_not_null', 'property_value_updated_at_not_null'),
    ('property_value', 'property_value_not_null', 'property_value_value_not_null')
  ) AS t(tbl, oldname, newname) LOOP
    IF to_regclass(r.tbl) IS NOT NULL
       AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.newname) THEN
      EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- Registry (property): property_type_* -> property_*.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('property', 'property_type_pkey', 'property_pkey'),
    ('property', 'property_type_handle_key', 'property_handle_key'),
    ('property', 'property_type_data_type_check', 'property_data_type_check'),
    ('property', 'property_type_kind_check', 'property_kind_check'),
    ('property', 'property_type_description_not_null', 'property_description_not_null'),
    ('property', 'property_type_id_not_null', 'property_id_not_null'),
    ('property', 'property_type_name_not_null', 'property_name_not_null'),
    ('property', 'property_type_official_not_null', 'property_official_not_null'),
    ('property', 'property_type_registered_at_not_null', 'property_registered_at_not_null'),
    ('property', 'property_type_data_type_not_null', 'property_value_type_not_null')
  ) AS t(tbl, oldname, newname) LOOP
    IF to_regclass(r.tbl) IS NOT NULL
       AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.tbl::regclass AND conname = r.newname) THEN
      EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;

-- Data and classifier tables: property_type_id FK objects -> property_id.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('metric', 'metric_property_type_id_fkey', 'metric_property_id_fkey'),
    ('metric', 'metric_property_type_id_not_null', 'metric_property_id_not_null'),
    ('state', 'state_property_type_id_fkey', 'state_property_id_fkey'),
    ('state', 'state_property_type_id_not_null', 'state_property_id_not_null'),
    ('event', 'event_property_type_id_fkey', 'event_property_id_fkey'),
    ('event', 'event_property_type_id_not_null', 'event_property_id_not_null'),
    ('product_property', 'product_property_property_type_id_fkey', 'product_property_property_id_fkey'),
    ('product_property', 'product_property_property_type_id_not_null', 'product_property_property_id_not_null'),
    ('product_property', 'product_property_product_id_property_type_id_key', 'product_property_product_id_property_id_key'),
    ('standard_property', 'standard_property_property_type_id_fkey', 'standard_property_property_id_fkey'),
    ('standard_property', 'standard_property_property_type_id_not_null', 'standard_property_property_id_not_null'),
    ('standard_property', 'standard_property_standard_id_property_type_id_key', 'standard_property_standard_id_property_id_key'),
    ('location_type_property', 'location_type_property_property_type_id_fkey', 'location_type_property_property_id_fkey'),
    ('location_type_property', 'location_type_property_property_type_id_not_null', 'location_type_property_property_id_not_null'),
    ('location_type_property', 'location_type_property_location_type_id_property_type_id_key', 'location_type_property_location_type_id_property_id_key')
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
    ('property_component_idx', 'property_value_component_idx'),
    ('product_property_property_type_idx', 'product_property_property_idx'),
    ('standard_property_property_type_idx', 'standard_property_property_idx'),
    ('location_type_property_property_type_idx', 'location_type_property_property_idx')
  ) AS t(oldname, newname) LOOP
    IF EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.oldname)
       AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = r.newname) THEN
      EXECUTE format('ALTER INDEX %I RENAME TO %I', r.oldname, r.newname);
    END IF;
  END LOOP;
END $$;
