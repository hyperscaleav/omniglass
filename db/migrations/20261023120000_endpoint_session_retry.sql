-- migrate:up

-- An operator's :retry of an endpoint's rejected held session (#1049, #1056
-- ruling) is an explicit counter the worklist carries, not the endpoint's
-- updated_at: an unrelated edit (a label) must not pull a rejected session
-- out of rejected. Idempotent.
ALTER TABLE public.endpoint ADD COLUMN IF NOT EXISTS session_retry bigint NOT NULL DEFAULT 0;

-- migrate:down

ALTER TABLE public.endpoint DROP COLUMN IF EXISTS session_retry;
