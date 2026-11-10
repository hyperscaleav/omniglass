-- migrate:up

-- A command's delivery history (#1626): dispatched_at is the last time a
-- node took it, so a redelivery overwrote the only stamp. first_dispatched_at
-- keeps the first take (the issue-to-dispatch latency reads from it) and
-- delivery_count counts the takes, so a redelivered command reads as one.
ALTER TABLE public.command ADD COLUMN IF NOT EXISTS first_dispatched_at timestamp with time zone;
ALTER TABLE public.command ADD COLUMN IF NOT EXISTS delivery_count integer DEFAULT 0 NOT NULL;

-- The deliverable commands: unexecuted, read by age. The pull and the queue
-- health read both resolve their candidates from these rows, which the
-- issued-status indexes do not cover (a fire-and-forget command is settled
-- yet still deliverable until it executes or ages out).
CREATE INDEX IF NOT EXISTS command_unexecuted_idx ON public.command (ts) WHERE executed_at IS NULL;

-- The redelivered commands, by when they were last taken: the queue health
-- read counts the redeliveries in its window, executed since or not, from
-- these few rows rather than the whole history.
CREATE INDEX IF NOT EXISTS command_redelivered_idx ON public.command (dispatched_at) WHERE delivery_count > 1;

-- migrate:down

DROP INDEX IF EXISTS public.command_redelivered_idx;
DROP INDEX IF EXISTS public.command_unexecuted_idx;
ALTER TABLE public.command DROP COLUMN IF EXISTS delivery_count;
ALTER TABLE public.command DROP COLUMN IF EXISTS first_dispatched_at;
