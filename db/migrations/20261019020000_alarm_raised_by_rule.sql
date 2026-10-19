-- migrate:up

-- An alarm names the rule that raised it (#919; ruling 3 on #819): nullable,
-- since a hand-raised alarm has none, and set null when the rule is deleted
-- so the incident's record survives its rule.
ALTER TABLE public.alarm ADD COLUMN IF NOT EXISTS raised_by_rule_id uuid;

DO $$
BEGIN
    ALTER TABLE public.alarm ADD CONSTRAINT alarm_raised_by_rule_id_fkey
        FOREIGN KEY (raised_by_rule_id) REFERENCES public.event_rule(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- migrate:down

ALTER TABLE public.alarm DROP CONSTRAINT IF EXISTS alarm_raised_by_rule_id_fkey;
ALTER TABLE public.alarm DROP COLUMN IF EXISTS raised_by_rule_id;
