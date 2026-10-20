-- migrate:up

-- An event a rule derived names the rule (#949): derived_by_rule_id, added
-- without a reference by ADR-0066 before the rule table existed, now
-- references event_rule, set null when the rule is deleted so the occurrence
-- survives it (source_rule keeps the rule's name). Nothing wrote the column
-- before the rules did, so every existing value is null.
DO $$
BEGIN
    ALTER TABLE public.event ADD CONSTRAINT event_derived_by_rule_id_fkey
        FOREIGN KEY (derived_by_rule_id) REFERENCES public.event_rule(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS event_derived_by_rule_id_idx ON public.event (derived_by_rule_id) WHERE derived_by_rule_id IS NOT NULL;

-- migrate:down

DROP INDEX IF EXISTS public.event_derived_by_rule_id_idx;
ALTER TABLE public.event DROP CONSTRAINT IF EXISTS event_derived_by_rule_id_fkey;
