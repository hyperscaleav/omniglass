-- migrate:up

-- ADR-0063: registries take the _type suffix, the bare noun holds the data.
-- The signal-definition registry becomes `property_type`; the latest-value
-- store (was `property_value`) takes the freed bare noun `property`. Every
-- data table's `property_id` FK references the registry, so it becomes
-- `property_type_id`. Table and column renames only; the stale dependent
-- object names (`property_pkey`, `property_value_series_key`, and the like)
-- are left for a separate cosmetic pass, as the collapse did.

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

-- migrate:down

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
