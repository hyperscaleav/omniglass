-- migrate:up

-- The instances a calc rule's reducer skipped because their newest sample
-- was older than the staleness bound (#996 review), as [{key, instance}],
-- beside the inputs that had no value at all.
ALTER TABLE public.calc_rule_state ADD COLUMN IF NOT EXISTS stale jsonb NOT NULL DEFAULT '[]'::jsonb;

-- migrate:down

ALTER TABLE public.calc_rule_state DROP COLUMN IF EXISTS stale;
