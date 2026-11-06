-- migrate:up
-- #1527: metric is range partitioned by UTC day instead of by month
-- (ADR-1527, amending ADR-1275). The compacted tier drops a day's raw rows
-- about two days after the day is compacted, and a month partition would hold
-- them up to a month whatever the compaction does; with a partition a day,
-- retention drops a whole day.
--
-- One create-copy-swap, in this one migration's transaction: the
-- month-partitioned table steps aside as metric_month (with its identity
-- sequence, so the new table's name is free), the day-partitioned table is
-- created LIKE it, so it has metric's columns as they stand when this runs
-- (types, defaults, identity) rather than as #1275 wrote them, one partition
-- for every UTC day the old table holds a sample in plus today and the three
-- after it (the server keeps three ahead from here, and checks at boot), every
-- sample is copied keeping its id, the ids carry on past the old table's, and
-- the copy is refused unless every sample arrived. Then the old table goes,
-- its month partitions with it, and its indexes are made again on the new
-- table from their own definitions, under their own names. A table already
-- partitioned by day is left as it is.

DO $$
DECLARE
  d date;
  had bigint;
  copied bigint;
  idx text[];
  def text;
BEGIN
  IF EXISTS (SELECT 1 FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
             WHERE i.inhparent = 'public.metric'::regclass AND c.relname ~ '^metric_p[0-9]{8}$') THEN
    RETURN;
  END IF;
  ALTER TABLE public.metric RENAME TO metric_month;
  ALTER SEQUENCE IF EXISTS public.metric_id_seq RENAME TO metric_month_id_seq;

  CREATE TABLE public.metric (LIKE public.metric_month INCLUDING ALL EXCLUDING INDEXES) PARTITION BY RANGE (ts);

  FOR d IN
    SELECT DISTINCT (ts AT TIME ZONE 'UTC')::date FROM public.metric_month
    UNION
    SELECT (now() AT TIME ZONE 'UTC')::date + g FROM generate_series(0, 3) AS g
  LOOP
    EXECUTE format('CREATE TABLE public.%I PARTITION OF public.metric FOR VALUES FROM (%L) TO (%L)',
      'metric_p' || to_char(d, 'YYYYMMDD'),
      to_char(d, 'YYYY-MM-DD') || ' 00:00:00+00',
      to_char(d + 1, 'YYYY-MM-DD') || ' 00:00:00+00');
  END LOOP;

  INSERT INTO public.metric OVERRIDING SYSTEM VALUE SELECT * FROM public.metric_month;

  -- Ids carry on from the old table's: a sample's id is what the event rule
  -- reconcile marks its place by.
  PERFORM setval(pg_get_serial_sequence('public.metric', 'id'),
    GREATEST((SELECT last_value FROM public.metric_month_id_seq), (SELECT COALESCE(max(id), 0) FROM public.metric), 1));

  SELECT count(*) INTO had FROM public.metric_month;
  SELECT count(*) INTO copied FROM public.metric;
  IF had <> copied THEN
    RAISE EXCEPTION 'metric day partitions: the month-partitioned table holds % samples and % were copied', had, copied;
  END IF;

  SELECT array_agg(pg_get_indexdef(indexrelid) ORDER BY indexrelid) INTO idx
    FROM pg_index WHERE indrelid = 'public.metric_month'::regclass;
  DROP TABLE public.metric_month;
  FOREACH def IN ARRAY coalesce(idx, '{}') LOOP
    EXECUTE replace(def, ' ON ONLY public.metric_month ', ' ON public.metric ');
  END LOOP;
END $$;

-- migrate:down
-- The same swap back to a partition a month: one for every UTC month the
-- table holds a sample in plus this month and the two after it, as #1275 left
-- it.
DO $$
DECLARE
  m date;
  had bigint;
  copied bigint;
  idx text[];
  def text;
BEGIN
  IF EXISTS (SELECT 1 FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
             WHERE i.inhparent = 'public.metric'::regclass AND c.relname ~ '^metric_p[0-9]{6}$') THEN
    RETURN;
  END IF;
  ALTER TABLE public.metric RENAME TO metric_day;
  ALTER SEQUENCE IF EXISTS public.metric_id_seq RENAME TO metric_day_id_seq;

  CREATE TABLE public.metric (LIKE public.metric_day INCLUDING ALL EXCLUDING INDEXES) PARTITION BY RANGE (ts);

  FOR m IN
    SELECT DISTINCT date_trunc('month', ts AT TIME ZONE 'UTC')::date FROM public.metric_day
    UNION
    SELECT (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => g))::date FROM generate_series(0, 2) AS g
  LOOP
    EXECUTE format('CREATE TABLE public.%I PARTITION OF public.metric FOR VALUES FROM (%L) TO (%L)',
      'metric_p' || to_char(m, 'YYYYMM'),
      to_char(m, 'YYYY-MM-DD') || ' 00:00:00+00',
      to_char(m + interval '1 month', 'YYYY-MM-DD') || ' 00:00:00+00');
  END LOOP;

  INSERT INTO public.metric OVERRIDING SYSTEM VALUE SELECT * FROM public.metric_day;

  PERFORM setval(pg_get_serial_sequence('public.metric', 'id'),
    GREATEST((SELECT last_value FROM public.metric_day_id_seq), (SELECT COALESCE(max(id), 0) FROM public.metric), 1));

  SELECT count(*) INTO had FROM public.metric_day;
  SELECT count(*) INTO copied FROM public.metric;
  IF had <> copied THEN
    RAISE EXCEPTION 'metric month partitions: the day-partitioned table holds % samples and % were copied', had, copied;
  END IF;

  SELECT array_agg(pg_get_indexdef(indexrelid) ORDER BY indexrelid) INTO idx
    FROM pg_index WHERE indrelid = 'public.metric_day'::regclass;
  DROP TABLE public.metric_day;
  FOREACH def IN ARRAY coalesce(idx, '{}') LOOP
    EXECUTE replace(def, ' ON ONLY public.metric_day ', ' ON public.metric ');
  END LOOP;
END $$;
