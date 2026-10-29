-- migrate:up

-- Snooze (#1318, ADR-1318): a fact about an alarm's CONDITION, the
-- (component, dedup_key) identity of ADR-0075, not about one alarm row and not
-- about a person. One row per snoozed condition, lasting until snoozed_until;
-- re-snoozing replaces the deadline. snoozed_by is the principal who set it,
-- its identifier resolved when read (as the acknowledger's is), so a purge
-- reads honestly empty while the audit row keeps the name. The deadline index
-- is what the notifier tick's end-of-snooze sweep reads.
CREATE TABLE IF NOT EXISTS public.alarm_snooze (
    component_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    dedup_key text NOT NULL,
    snoozed_until timestamp with time zone NOT NULL,
    snoozed_by uuid REFERENCES public.principal(id) ON DELETE SET NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (component_id, dedup_key)
);
CREATE INDEX IF NOT EXISTS alarm_snooze_until_idx ON public.alarm_snooze (snoozed_until);
CREATE INDEX IF NOT EXISTS alarm_snooze_by_idx ON public.alarm_snooze (snoozed_by) WHERE snoozed_by IS NOT NULL;

-- migrate:down

DROP TABLE IF EXISTS public.alarm_snooze;
