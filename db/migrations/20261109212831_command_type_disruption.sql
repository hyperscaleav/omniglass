-- migrate:up
-- #1651: a disruptive command type (reboot) closes the gates of the endpoints
-- it takes down at delivery. quiet_seconds is how long the node runs nothing
-- against them; after it, each endpoint reopens on its own first up verdict,
-- and hold_seconds is the counted backstop that reopens one that never reads
-- up. Neither is the settle window, which is how long a settleable command's
-- outcome takes to show. Every existing row is non-disruptive by default.
ALTER TABLE public.command_type
    ADD COLUMN IF NOT EXISTS disruptive boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS quiet_seconds integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS hold_seconds integer DEFAULT 0 NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'command_type_disruption_check') THEN
        ALTER TABLE public.command_type ADD CONSTRAINT command_type_disruption_check CHECK (
            quiet_seconds >= 0 AND hold_seconds >= 0
            AND (disruptive OR (quiet_seconds = 0 AND hold_seconds = 0))
            AND (NOT disruptive OR hold_seconds > quiet_seconds));
    END IF;
END $$;

-- migrate:down
ALTER TABLE public.command_type DROP CONSTRAINT IF EXISTS command_type_disruption_check;
ALTER TABLE public.command_type
    DROP COLUMN IF EXISTS hold_seconds,
    DROP COLUMN IF EXISTS quiet_seconds,
    DROP COLUMN IF EXISTS disruptive;
