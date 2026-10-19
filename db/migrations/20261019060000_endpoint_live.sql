-- migrate:up

-- When an endpoint last delivered anything (#933): one row per endpoint that
-- has, stamped with the server's clock by the ingest batch that carried it,
-- whatever the lane, the series or the sample's own timestamp. The stamp is
-- coalesced (a batch skips a row stamped within a few seconds, or locked by
-- another batch stamping it), so the hot path pays one statement per batch,
-- never a write per sample. Kept off endpoint, whose row an operator edits,
-- so ingest never waits on an edit. The same shape as event_rule_live.
CREATE TABLE IF NOT EXISTS public.endpoint_live (
    endpoint_id uuid PRIMARY KEY REFERENCES public.endpoint(id) ON DELETE CASCADE,
    last_seen_at timestamp with time zone NOT NULL
);

-- migrate:down

DROP TABLE IF EXISTS public.endpoint_live;
