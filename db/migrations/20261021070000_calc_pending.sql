-- migrate:up

-- A calc rule's pending work (#996, epic #525): one row per rule and owner
-- whose inputs changed since the rule last computed there. The sample-write
-- transaction inserts a mark for every enabled rule reading one of the
-- batch's keys (coalescing: a mark already present is kept, so marked_at is
-- the oldest unserved change), and the server's calc tick drains marks with
-- FOR UPDATE SKIP LOCKED, computing and deleting each in one transaction, so
-- a restart loses nothing and two replicas never compute one mark twice.
-- The owner is an exclusive arc (component or system), each cascading, so a
-- deleted owner or rule drops its marks.
CREATE TABLE IF NOT EXISTS public.calc_pending (
    rule_id uuid NOT NULL REFERENCES public.calc_rule(id) ON DELETE CASCADE,
    owner_kind text NOT NULL CONSTRAINT calc_pending_owner_kind_check CHECK (owner_kind IN ('component', 'system')),
    component_id uuid REFERENCES public.component(id) ON DELETE CASCADE,
    system_id uuid REFERENCES public.system(id) ON DELETE CASCADE,
    marked_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT calc_pending_owner_check CHECK (
        (owner_kind = 'component' AND component_id IS NOT NULL AND system_id IS NULL)
        OR (owner_kind = 'system' AND system_id IS NOT NULL AND component_id IS NULL)),
    CONSTRAINT calc_pending_key UNIQUE NULLS NOT DISTINCT (rule_id, owner_kind, component_id, system_id)
);

CREATE INDEX IF NOT EXISTS calc_pending_marked_idx ON public.calc_pending (marked_at);

-- A calc rule's last outcome on each owner, written by the tick: when it
-- computed, at which rule version, the value it reached (null when it could
-- not), the inputs that had no current value, and any other error. The rule
-- state read (#998) answers from it.
CREATE TABLE IF NOT EXISTS public.calc_rule_state (
    rule_id uuid NOT NULL REFERENCES public.calc_rule(id) ON DELETE CASCADE,
    owner_kind text NOT NULL CONSTRAINT calc_rule_state_owner_kind_check CHECK (owner_kind IN ('component', 'system')),
    component_id uuid REFERENCES public.component(id) ON DELETE CASCADE,
    system_id uuid REFERENCES public.system(id) ON DELETE CASCADE,
    computed_at timestamp with time zone NOT NULL,
    rule_version integer NOT NULL,
    value jsonb,
    missing jsonb NOT NULL DEFAULT '[]'::jsonb,
    error text NOT NULL DEFAULT '',
    CONSTRAINT calc_rule_state_owner_check CHECK (
        (owner_kind = 'component' AND component_id IS NOT NULL AND system_id IS NULL)
        OR (owner_kind = 'system' AND system_id IS NOT NULL AND component_id IS NULL)),
    CONSTRAINT calc_rule_state_key UNIQUE NULLS NOT DISTINCT (rule_id, owner_kind, component_id, system_id)
);

-- migrate:down

DROP TABLE IF EXISTS public.calc_rule_state;
DROP TABLE IF EXISTS public.calc_pending;
