-- migrate:up

-- component_model's real product identity is (make_id, model_number), not the
-- kebab id alone: the id stays the addressable handle, but nothing today
-- stops two rows naming the same make + model number under different ids, or
-- a row with a blank model_number. Close both. Idempotent (guarded against
-- partial state).

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'component_model_make_model_key'
  ) THEN
    ALTER TABLE component_model ADD CONSTRAINT component_model_make_model_key UNIQUE (make_id, model_number);
  END IF;
END $$;

ALTER TABLE component_model ALTER COLUMN model_number DROP DEFAULT;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'component_model_model_number_nonempty'
  ) THEN
    ALTER TABLE component_model ADD CONSTRAINT component_model_model_number_nonempty CHECK (model_number <> '');
  END IF;
END $$;

-- The new composite UNIQUE constraint's backing index already covers a
-- make_id-prefix lookup, so the standalone index is redundant.
DROP INDEX IF EXISTS component_model_make_id_idx;

-- migrate:down

CREATE INDEX IF NOT EXISTS component_model_make_id_idx ON component_model(make_id);
ALTER TABLE component_model DROP CONSTRAINT IF EXISTS component_model_model_number_nonempty;
ALTER TABLE component_model ALTER COLUMN model_number SET DEFAULT '';
ALTER TABLE component_model DROP CONSTRAINT IF EXISTS component_model_make_model_key;
