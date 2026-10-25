-- migrate:up

-- A component's log line keeps when it arrived beside the time it claims
-- (#1145): a driver may read a line's time out of its payload, so ts is the
-- component's account and received_at the server's receipt, and the console
-- shows both where they differ. Rows from before this column read null (their
-- arrival was not recorded), and every new row takes the database's now. Pure
-- DDL, idempotent.
ALTER TABLE public.log_line ADD COLUMN IF NOT EXISTS received_at timestamptz;
ALTER TABLE public.log_line ALTER COLUMN received_at SET DEFAULT now();

-- The component and system log reads window and order on arrival, the line's
-- receipt where it was kept and its own time before, so a component whose
-- clock reset (its lines claiming a year long past) still reads in the last
-- day. One expression index serves both.
CREATE INDEX IF NOT EXISTS log_line_component_arrival_idx
    ON public.log_line (component_id, (coalesce(received_at, ts)) DESC);

-- migrate:down

DROP INDEX IF EXISTS public.log_line_component_arrival_idx;
ALTER TABLE public.log_line DROP COLUMN IF EXISTS received_at;
