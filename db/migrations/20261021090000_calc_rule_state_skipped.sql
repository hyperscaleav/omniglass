-- migrate:up

-- The members a system calc rule skipped because they held no value of an
-- input (#997), as [{key, component}], beside the inputs no member held.
ALTER TABLE public.calc_rule_state ADD COLUMN IF NOT EXISTS skipped jsonb NOT NULL DEFAULT '[]'::jsonb;

-- migrate:down

ALTER TABLE public.calc_rule_state DROP COLUMN IF EXISTS skipped;
