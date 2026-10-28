-- migrate:up

-- The action rule and the notification delivery row (#618, #1321).
--
-- An action_rule is the subscription that wires an alarm transition to a
-- notification, kept apart from the event_rule that detected it: ON an alarm
-- transition (raised, cleared, or both), WHEN an expression over the alarm
-- facts holds (empty: always), SEND a notification type (NULL: the official
-- one for the transition) TO its targets (action_rule_target). In #618 it has
-- one step, notify; flows extend it rather than replace it. A rule row
-- carries the official boolean every rule row carries; none ship.
CREATE TABLE IF NOT EXISTS public.action_rule (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    name text NOT NULL CONSTRAINT action_rule_name_key UNIQUE,
    label text,
    description text NOT NULL DEFAULT '',
    on_transitions text[] NOT NULL,
    when_expr text NOT NULL DEFAULT '',
    notification_type_id uuid REFERENCES public.notification_type(id) ON DELETE RESTRICT,
    enabled boolean NOT NULL DEFAULT true,
    official boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT action_rule_on_check CHECK (cardinality(on_transitions) > 0
        AND on_transitions <@ ARRAY['raised', 'cleared']::text[])
);

-- The targets a rule sends to. A target a rule still names cannot be deleted
-- (RESTRICT); deleting the rule lets go of its targets.
CREATE TABLE IF NOT EXISTS public.action_rule_target (
    rule_id uuid NOT NULL REFERENCES public.action_rule(id) ON DELETE CASCADE,
    target_id uuid NOT NULL REFERENCES public.notification_target(id) ON DELETE RESTRICT,
    PRIMARY KEY (rule_id, target_id)
);
CREATE INDEX IF NOT EXISTS action_rule_target_target_idx ON public.action_rule_target (target_id);

-- A notification is one message on its way: a stateful row holding its own
-- delivery state (queued, sent, failed), written in the SAME transaction as
-- the alarm transition it is about, rendered then (so a retry sends the same
-- bytes and a preview is what a delivery carries), and drained by a server
-- tick (FOR UPDATE SKIP LOCKED over the due rows, ADR-0618). The rule, type
-- and target are kept by name beside their uuids, so the history reads after
-- any of them is deleted. component_id is the alarm's, denormalized for the
-- scope filter every read injects.
CREATE TABLE IF NOT EXISTS public.notification (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    alarm_id uuid NOT NULL REFERENCES public.alarm(id) ON DELETE CASCADE,
    component_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    transition text NOT NULL CONSTRAINT notification_transition_check CHECK (transition IN ('raised', 'cleared')),
    rule_id uuid REFERENCES public.action_rule(id) ON DELETE SET NULL,
    rule_name text NOT NULL,
    notification_type_id uuid REFERENCES public.notification_type(id) ON DELETE SET NULL,
    type_name text NOT NULL,
    target_id uuid REFERENCES public.notification_target(id) ON DELETE SET NULL,
    target_name text NOT NULL,
    subject text NOT NULL DEFAULT '',
    text text NOT NULL DEFAULT '',
    facts jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'queued' CONSTRAINT notification_status_check CHECK (status IN ('queued', 'sent', 'failed')),
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamp with time zone NOT NULL DEFAULT now(),
    last_status integer,
    last_error text,
    sent_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);
-- The due set the tick drains, and nothing else: a sent or failed row is out
-- of the index.
CREATE INDEX IF NOT EXISTS notification_due_idx ON public.notification (next_attempt_at, id) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS notification_alarm_idx ON public.notification (alarm_id, created_at);
CREATE INDEX IF NOT EXISTS notification_component_idx ON public.notification (component_id, created_at DESC);
CREATE INDEX IF NOT EXISTS notification_rule_idx ON public.notification (rule_id) WHERE rule_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS notification_type_idx ON public.notification (notification_type_id) WHERE notification_type_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS notification_target_idx ON public.notification (target_id) WHERE target_id IS NOT NULL;

-- migrate:down

DROP TABLE IF EXISTS public.notification;
DROP TABLE IF EXISTS public.action_rule_target;
DROP TABLE IF EXISTS public.action_rule;
