-- migrate:up

-- The rows the one-writer ruling disowns (ADR-0161, ADR-0163): a sample or
-- event row a one-time backfill found its key's writer class would refuse
-- today is moved here, whole, with the reason, rather than deleted. It leaves
-- every read (none serves this table; the pin read only checks a retired
-- pin's key against it), and the record survives for audit: a retention prune never deletes a declared row or a series' latest
-- (ADR-0080), and retirement is not a prune.
CREATE TABLE IF NOT EXISTS public.retired_sample (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lane text NOT NULL CHECK (lane IN ('metric', 'property', 'event')),
    sample_id bigint NOT NULL,
    sample jsonb NOT NULL,
    reason text NOT NULL,
    retired_at timestamptz NOT NULL DEFAULT now(),
    -- healed_at is when the boot heal re-judged the event rule series this
    -- row's retirement touched; null until it has, so the heal runs once.
    healed_at timestamptz,
    UNIQUE (lane, sample_id)
);

-- A retired planted pin's key is never re-learned as the pin from the
-- observed series (ADR-0163): the pin read looks its endpoint up here.
CREATE INDEX IF NOT EXISTS retired_sample_pin_idx
    ON public.retired_sample ((sample->>'component_id'), (sample->>'instance'))
    WHERE lane = 'property' AND sample->>'provenance' = 'declared';

-- migrate:down

DROP TABLE IF EXISTS public.retired_sample;
