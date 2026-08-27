-- migrate:up

-- The driver spec (#813): a driver row gains its declarative body (data, not
-- code; NULL for a stub that has not been authored yet), and an endpoint
-- records the attachment that authored it: the driver it came from and the
-- inputs supplied at attach (secret inputs by reference name, never a value).
ALTER TABLE driver ADD COLUMN IF NOT EXISTS spec jsonb;
ALTER TABLE endpoint ADD COLUMN IF NOT EXISTS driver_id uuid REFERENCES driver(id);
ALTER TABLE endpoint ADD COLUMN IF NOT EXISTS inputs jsonb DEFAULT '{}'::jsonb NOT NULL;

-- migrate:down

ALTER TABLE endpoint DROP COLUMN IF EXISTS inputs;
ALTER TABLE endpoint DROP COLUMN IF EXISTS driver_id;
ALTER TABLE driver DROP COLUMN IF EXISTS spec;
