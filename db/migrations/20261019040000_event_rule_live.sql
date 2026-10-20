-- migrate:up

-- When a rule last judged a sample (#929): one row per rule that has judged
-- one, written by the ingest batch that judged it, at most once per rule per
-- few seconds (a batch skips a row stamped within that bound, or locked by
-- another batch stamping it), so the hot path pays one statement per batch
-- and never a write per sample. Kept off event_rule, whose row every ingest
-- holds FOR SHARE: stamping it there would serialize the batches on it.
CREATE TABLE IF NOT EXISTS public.event_rule_live (
    rule_id uuid PRIMARY KEY REFERENCES public.event_rule(id) ON DELETE CASCADE,
    last_judged_at timestamp with time zone NOT NULL
);

-- migrate:down

DROP TABLE IF EXISTS public.event_rule_live;
