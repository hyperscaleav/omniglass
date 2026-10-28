-- migrate:up
-- #417 (its third PR): a metric or property type's own retention window, in
-- whole days, overriding its lane's (retention.metric_days, property_days). Null
-- is the lane's window. Set through the types' setRetention method, which works
-- on an official type too: a shipped type's definition is the seed's, how long
-- an install keeps its samples is the install's, and the boot seed's upsert
-- never writes this column. The range is the retention settings' (1 to 36,500
-- days: a cutoff further back overflows the timestamp encoding).
ALTER TABLE public.metric_type ADD COLUMN IF NOT EXISTS retention_days integer;
ALTER TABLE public.property_type ADD COLUMN IF NOT EXISTS retention_days integer;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'metric_type_retention_days_check') THEN
    ALTER TABLE public.metric_type ADD CONSTRAINT metric_type_retention_days_check
      CHECK (retention_days IS NULL OR retention_days BETWEEN 1 AND 36500);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'property_type_retention_days_check') THEN
    ALTER TABLE public.property_type ADD CONSTRAINT property_type_retention_days_check
      CHECK (retention_days IS NULL OR retention_days BETWEEN 1 AND 36500);
  END IF;
END $$;

-- migrate:down
ALTER TABLE public.property_type DROP CONSTRAINT IF EXISTS property_type_retention_days_check;
ALTER TABLE public.metric_type DROP CONSTRAINT IF EXISTS metric_type_retention_days_check;
ALTER TABLE public.property_type DROP COLUMN IF EXISTS retention_days;
ALTER TABLE public.metric_type DROP COLUMN IF EXISTS retention_days;
