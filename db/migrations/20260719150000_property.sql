-- migrate:up

-- Generalize the datapoint_type catalog into the primitive-agnostic property
-- registry: the typed keyspace a datapoint observes and a field declares. Every
-- consumer keys by the name string (no FK), so the rename preserves behavior.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'datapoint_type')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property') THEN
    ALTER TABLE datapoint_type RENAME TO property;
  END IF;
END $$;

-- value_type -> data_type; backfill text -> string; widen the set to add bool.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'property' AND column_name = 'value_type') THEN
    ALTER TABLE property RENAME COLUMN value_type TO data_type;
  END IF;
END $$;
ALTER TABLE property DROP CONSTRAINT IF EXISTS datapoint_type_value_type_check;
UPDATE property SET data_type = 'string' WHERE data_type = 'text';
ALTER TABLE property ADD CONSTRAINT property_data_type_check
  CHECK (data_type IN ('string', 'int', 'float', 'bool', 'json'));

-- kind becomes optional (a declared-only key has no observed kind).
ALTER TABLE property ALTER COLUMN kind DROP NOT NULL;

-- official replaces the scope ladder; existing (all seed) rows are official.
ALTER TABLE property ADD COLUMN IF NOT EXISTS official boolean NOT NULL DEFAULT false;
UPDATE property SET official = true;

-- Collapse the (scope, name) key to a plain name PK; drop the unused ladder columns.
ALTER TABLE property DROP CONSTRAINT IF EXISTS datapoint_type_pkey;
ALTER TABLE property DROP CONSTRAINT IF EXISTS datapoint_type_scope_check;
ALTER TABLE property DROP COLUMN IF EXISTS scope;
ALTER TABLE property DROP COLUMN IF EXISTS template_id;
ALTER TABLE property ADD PRIMARY KEY (name);

-- migrate:down

-- Drop the new-model rows datapoint_type cannot represent: a declared-only key
-- (no observed kind) and a bool-typed key have no place in the restored NOT NULL
-- kind and {int,float,text,json} shape. They re-seed on the next up.
DELETE FROM property WHERE kind IS NULL OR data_type = 'bool';

ALTER TABLE property DROP CONSTRAINT IF EXISTS property_pkey;
ALTER TABLE property ADD COLUMN IF NOT EXISTS template_id uuid;
ALTER TABLE property ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'official';
ALTER TABLE property ADD CONSTRAINT datapoint_type_scope_check CHECK (scope IN ('official', 'org', 'template'));
ALTER TABLE property ADD PRIMARY KEY (scope, name);
ALTER TABLE property DROP COLUMN IF EXISTS official;
ALTER TABLE property ALTER COLUMN kind SET NOT NULL;
ALTER TABLE property DROP CONSTRAINT IF EXISTS property_data_type_check;
UPDATE property SET data_type = 'text' WHERE data_type = 'string';
ALTER TABLE property ADD CONSTRAINT datapoint_type_value_type_check CHECK (data_type IN ('int', 'float', 'text', 'json'));
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'property' AND column_name = 'data_type') THEN
    ALTER TABLE property RENAME COLUMN data_type TO value_type;
  END IF;
END $$;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'property') THEN
    ALTER TABLE property RENAME TO datapoint_type;
  END IF;
END $$;
