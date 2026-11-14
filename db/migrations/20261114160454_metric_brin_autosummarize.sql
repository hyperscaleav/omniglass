-- migrate:up

-- The block range indexes on metric summarize a range once it fills (#1929).
-- An unsummarized range reads as a match, so a day nobody summarized cost
-- the event rule reconcile its whole heap on every pass. A partitioned index
-- takes no ALTER ... SET, so each index is made again with the option, which
-- every day's index inherits, those attached later included. An index that
-- already carries it is left alone, so a second run changes nothing.
DO $$
DECLARE
  col text;
BEGIN
  FOREACH col IN ARRAY ARRAY['id', 'ts'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relname = 'metric_' || col || '_brin'
        AND c.reloptions @> ARRAY['autosummarize=on']) THEN
      EXECUTE format('DROP INDEX IF EXISTS public.%I', 'metric_' || col || '_brin');
      EXECUTE format('CREATE INDEX %I ON public.metric USING brin (%I) WITH (autosummarize = on)',
        'metric_' || col || '_brin', col);
    END IF;
  END LOOP;
END $$;

-- migrate:down

DO $$
DECLARE
  col text;
BEGIN
  FOREACH col IN ARRAY ARRAY['id', 'ts'] LOOP
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relname = 'metric_' || col || '_brin'
        AND c.reloptions @> ARRAY['autosummarize=on']) THEN
      EXECUTE format('DROP INDEX public.%I', 'metric_' || col || '_brin');
      EXECUTE format('CREATE INDEX %I ON public.metric USING brin (%I)', 'metric_' || col || '_brin', col);
    END IF;
  END LOOP;
END $$;
