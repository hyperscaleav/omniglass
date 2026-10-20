-- migrate:up

-- An event rule's alarm is optional and its events are typed (#948, epic
-- #938). A rule with no severity is informational: it raises no alarm. A
-- rule names the event type it writes when its condition starts
-- (fire_event_type_id, required once the following backfill has given every
-- existing rule one) and, optionally, the type it writes when the condition
-- ends (clear_event_type_id). Both reference the event_type registry by id,
-- so a type renamed keeps its rules, and one a rule names cannot be deleted.
ALTER TABLE public.event_rule ALTER COLUMN severity DROP NOT NULL;
ALTER TABLE public.event_rule ADD COLUMN IF NOT EXISTS fire_event_type_id uuid;
ALTER TABLE public.event_rule ADD COLUMN IF NOT EXISTS clear_event_type_id uuid;

DO $$
BEGIN
    ALTER TABLE public.event_rule ADD CONSTRAINT event_rule_fire_event_type_id_fkey
        FOREIGN KEY (fire_event_type_id) REFERENCES public.event_type(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE public.event_rule ADD CONSTRAINT event_rule_clear_event_type_id_fkey
        FOREIGN KEY (clear_event_type_id) REFERENCES public.event_type(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- An event carries the alarm it opened or resolved: a rule's fire event the
-- alarm it opened, its clear event the alarm it resolved. Nullable (most
-- events touch no alarm) and set null if the alarm row goes, so the
-- occurrence survives it.
ALTER TABLE public.event ADD COLUMN IF NOT EXISTS alarm_id uuid;

DO $$
BEGIN
    ALTER TABLE public.event ADD CONSTRAINT event_alarm_id_fkey
        FOREIGN KEY (alarm_id) REFERENCES public.alarm(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS event_alarm_id_idx ON public.event (alarm_id) WHERE alarm_id IS NOT NULL;

-- A rule's condition in force on one component and instance, since when:
-- one row while the condition holds, for every rule, informational or not.
-- It is the rule's own in-force state (an alarm-raising rule opens and
-- resolves its alarm beside it), so a rule with no alarm still writes its
-- clear event once, at the edge. Keyed and cascading like event_rule_state.
CREATE TABLE IF NOT EXISTS public.event_rule_condition (
    rule_id uuid NOT NULL REFERENCES public.event_rule(id) ON DELETE CASCADE,
    component_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    instance text NOT NULL DEFAULT '',
    since timestamp with time zone NOT NULL,
    PRIMARY KEY (rule_id, component_id, instance)
);

-- migrate:down

DROP TABLE IF EXISTS public.event_rule_condition;
DROP INDEX IF EXISTS public.event_alarm_id_idx;
ALTER TABLE public.event DROP CONSTRAINT IF EXISTS event_alarm_id_fkey;
ALTER TABLE public.event DROP COLUMN IF EXISTS alarm_id;
ALTER TABLE public.event_rule DROP CONSTRAINT IF EXISTS event_rule_clear_event_type_id_fkey;
ALTER TABLE public.event_rule DROP CONSTRAINT IF EXISTS event_rule_fire_event_type_id_fkey;
ALTER TABLE public.event_rule DROP COLUMN IF EXISTS clear_event_type_id;
ALTER TABLE public.event_rule DROP COLUMN IF EXISTS fire_event_type_id;
ALTER TABLE public.event_rule ALTER COLUMN severity SET NOT NULL;
