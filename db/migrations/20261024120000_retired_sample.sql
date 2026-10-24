-- migrate:up

-- The rows the one-writer ruling disowns (ADR-0161, ADR-0163): a sample or
-- event row a one-time backfill found its key's writer class would refuse
-- today is moved here, whole, with the reason, rather than deleted. It leaves
-- every read (none of them reads this table), and the record survives for
-- audit: a retention prune never deletes a declared row or a series' latest
-- (ADR-0080), and retirement is not a prune.
CREATE TABLE IF NOT EXISTS public.retired_sample (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lane text NOT NULL CHECK (lane IN ('metric', 'property', 'event')),
    sample_id bigint NOT NULL,
    sample jsonb NOT NULL,
    reason text NOT NULL,
    retired_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (lane, sample_id)
);

-- migrate:down

DROP TABLE IF EXISTS public.retired_sample;
