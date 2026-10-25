-- migrate:up

-- A component's log line keeps when it arrived beside the time it claims
-- (#1145): a driver may read a line's time out of its payload, so ts is the
-- component's account and received_at the server's receipt, and the console
-- shows both where they differ. Rows from before this column read null (their
-- arrival was not recorded), and every new row takes the database's now. Pure
-- DDL, idempotent.
ALTER TABLE public.log_line ADD COLUMN IF NOT EXISTS received_at timestamptz;
ALTER TABLE public.log_line ALTER COLUMN received_at SET DEFAULT now();

-- migrate:down

ALTER TABLE public.log_line DROP COLUMN IF EXISTS received_at;
