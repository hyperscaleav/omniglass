-- migrate:up
-- #1986: when a server's event rule sweep last completed, and when its peer
-- sweep timer last ran, both on the database's clock, so a second server
-- reads a peer's sweep age from the shared database and judges it only while
-- that peer is alive. Both null before a server's first pass and after it
-- stops cleanly; a server that died stops stamping seen_at and drops out of
-- its peers' read.
ALTER TABLE public.platform_server ADD COLUMN IF NOT EXISTS rule_swept_at timestamptz;
ALTER TABLE public.platform_server ADD COLUMN IF NOT EXISTS seen_at timestamptz;

-- migrate:down
ALTER TABLE public.platform_server DROP COLUMN IF EXISTS seen_at;
ALTER TABLE public.platform_server DROP COLUMN IF EXISTS rule_swept_at;
