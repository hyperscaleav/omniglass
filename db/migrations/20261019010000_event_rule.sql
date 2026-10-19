-- migrate:up

-- The event rule (#918, #819; ruling 1: a per-kind table): the authored
-- config that raises alarms. It reads one sample key (a property or a
-- metric), fires when fire_criteria holds for fire_sustain and clears when
-- clear_criteria holds for clear_sustain (absent: the negation of fire), at
-- a severity level named in the registry. selector picks the components it
-- watches (component types and explicit components; empty, all of them).
-- Criteria are the placeholder comparison ({op, value}) until the expression
-- engine (#524) lands; the jsonb column takes the expression without a
-- schema change. version bumps on every update.
CREATE TABLE IF NOT EXISTS public.event_rule (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    name text NOT NULL CONSTRAINT event_rule_name_key UNIQUE,
    label text,
    description text NOT NULL DEFAULT '',
    selector jsonb NOT NULL DEFAULT '{}'::jsonb,
    sample_key text NOT NULL,
    sample_lane text NOT NULL CHECK (sample_lane IN ('property', 'metric')),
    fire_criteria jsonb NOT NULL,
    clear_criteria jsonb,
    fire_sustain interval NOT NULL DEFAULT '0' CHECK (fire_sustain >= '0'),
    clear_sustain interval NOT NULL DEFAULT '0' CHECK (clear_sustain >= '0'),
    severity text NOT NULL CONSTRAINT event_rule_severity_fkey REFERENCES public.severity_level(name) ON UPDATE CASCADE,
    message text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    official boolean NOT NULL DEFAULT false,
    version integer NOT NULL DEFAULT 1,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS event_rule_sample_key_idx ON public.event_rule (sample_key) WHERE enabled;

-- migrate:down

DROP TABLE IF EXISTS public.event_rule;
