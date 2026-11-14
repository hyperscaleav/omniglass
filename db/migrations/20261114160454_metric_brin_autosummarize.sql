-- migrate:up

-- The block range indexes on metric summarize a range once it fills (#1929).
-- An unsummarized range reads as a match, so a day nobody summarized cost
-- the event rule reconcile its whole heap on every pass. A partitioned index
-- takes no ALTER ... SET, so the option goes on each day's own index, which
-- takes only a SHARE UPDATE EXCLUSIVE lock and rewrites nothing; the server
-- makes every day after this one with it (createMetricPartition). Setting it
-- again changes nothing.
DO $$
DECLARE
  leaf regclass;
BEGIN
  FOR leaf IN SELECT i.inhrelid::regclass FROM pg_inherits i
      WHERE i.inhparent IN (SELECT to_regclass('public.metric_id_brin') UNION ALL SELECT to_regclass('public.metric_ts_brin')) LOOP
    EXECUTE format('ALTER INDEX %s SET (autosummarize = on)', leaf);
  END LOOP;
END $$;

-- migrate:down

DO $$
DECLARE
  leaf regclass;
BEGIN
  FOR leaf IN SELECT i.inhrelid::regclass FROM pg_inherits i
      WHERE i.inhparent IN (SELECT to_regclass('public.metric_id_brin') UNION ALL SELECT to_regclass('public.metric_ts_brin')) LOOP
    EXECUTE format('ALTER INDEX %s RESET (autosummarize)', leaf);
  END LOOP;
END $$;
