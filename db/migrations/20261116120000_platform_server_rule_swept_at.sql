-- migrate:up
-- #1986: when a server's event rule sweep last completed, on the database's
-- clock, so a second server reads a peer's sweep age from the shared
-- database. Null before a server's first sweep and after it stops cleanly,
-- neither of which a peer judges.
ALTER TABLE public.platform_server ADD COLUMN IF NOT EXISTS rule_swept_at timestamptz;

-- migrate:down
ALTER TABLE public.platform_server DROP COLUMN IF EXISTS rule_swept_at;
