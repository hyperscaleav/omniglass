-- migrate:up

-- A rule's pending streak on one component and instance (#920): the action
-- its samples have pointed at, without a break, since "since". The
-- evaluator writes it as samples land and the sweep acts on it once it has
-- held for the rule's sustain, so a sustain survives a restart and works on
-- a transition-only input that sends no further sample. A row exists only
-- while a streak is pending.
CREATE TABLE IF NOT EXISTS public.event_rule_state (
    rule_id uuid NOT NULL REFERENCES public.event_rule(id) ON DELETE CASCADE,
    component_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    instance text NOT NULL DEFAULT '',
    pending text NOT NULL CHECK (pending IN ('fire', 'clear')),
    since timestamp with time zone NOT NULL,
    PRIMARY KEY (rule_id, component_id, instance)
);

-- migrate:down

DROP TABLE IF EXISTS public.event_rule_state;
