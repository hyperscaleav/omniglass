-- migrate:up

-- alarm.severity names a level in the registry (#917, ruling 4 on #819:
-- by name, so a rename carries every alarm with it). NOT VALID because the
-- levels are boot-seeded rows, which land after migrations run: the
-- reference is enforced on every write from here on, and the backfill
-- before it already mapped every existing row onto a seeded name.
DO $$
BEGIN
    ALTER TABLE public.alarm ADD CONSTRAINT alarm_severity_fkey
        FOREIGN KEY (severity) REFERENCES public.severity_level(name) ON UPDATE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- migrate:down

ALTER TABLE public.alarm DROP CONSTRAINT IF EXISTS alarm_severity_fkey;
