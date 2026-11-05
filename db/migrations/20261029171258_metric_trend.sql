-- migrate:up
-- #1276: the trend tier. One row per observed metric series per hour, holding
-- what a chart needs to draw the hour honestly at any zoom (count, min, max,
-- sum, first, last), written by the server's compaction once the hour is past
-- the late-data horizon, and kept for retention.trend_days, a window of its own
-- that nothing forces to exceed the samples'. ADR-1276.
CREATE TABLE IF NOT EXISTS public.metric_trend (
    series_id bigint NOT NULL REFERENCES public.metric_series(id) ON DELETE CASCADE,
    hour timestamp with time zone NOT NULL,
    count integer NOT NULL,
    min double precision NOT NULL,
    max double precision NOT NULL,
    sum double precision NOT NULL,
    first double precision NOT NULL,
    last double precision NOT NULL,
    CONSTRAINT metric_trend_pkey PRIMARY KEY (series_id, hour),
    CONSTRAINT metric_trend_count_check CHECK (count > 0)
);
-- The trend window's chunked prune finds old hours through it.
CREATE INDEX IF NOT EXISTS metric_trend_hour_brin ON public.metric_trend USING brin (hour);

-- A (series, hour) the compaction had announced when a sample landed in it: set
-- in the sample's own insert, read and cleared by a compaction once the hour is
-- folded, which recomputes the hour.
CREATE TABLE IF NOT EXISTS public.metric_trend_dirty (
    series_id bigint NOT NULL,
    hour timestamp with time zone NOT NULL,
    CONSTRAINT metric_trend_dirty_pkey PRIMARY KEY (series_id, hour)
);

-- How far the compaction has closed hours, as two marks. announced_through is
-- how far it has said it will close: a sample dated before it dirties its hour.
-- compacted_through is how far it has folded: every hour before it has its
-- trend, and no prune passes it. compacted_through never passes
-- announced_through; it moves an hour at a time, in the transaction that folds
-- the hour. One row, written by the compaction (absent until its first pass).
CREATE TABLE IF NOT EXISTS public.metric_trend_mark (
    id boolean DEFAULT true NOT NULL,
    announced_through timestamp with time zone NOT NULL,
    compacted_through timestamp with time zone NOT NULL,
    CONSTRAINT metric_trend_mark_pkey PRIMARY KEY (id),
    CONSTRAINT metric_trend_mark_one_row CHECK (id),
    CONSTRAINT metric_trend_mark_order CHECK (compacted_through <= announced_through)
);

-- What a metric insert dirties against: the announced mark, read after taking
-- the trend lock shared, so a compaction that takes the lock exclusive after
-- announcing knows every insert still to read the mark reads the new one.
-- VOLATILE and plpgsql on purpose: the read takes a fresh snapshot, after the
-- lock is held, even inside the insert statement that calls it.
CREATE OR REPLACE FUNCTION public.metric_trend_announced() RETURNS timestamp with time zone
LANGUAGE plpgsql VOLATILE AS $f$
DECLARE
  through timestamp with time zone;
BEGIN
  PERFORM pg_advisory_xact_lock_shared(hashtextextended('metric_trend:announce', 0));
  SELECT announced_through INTO through FROM public.metric_trend_mark;
  RETURN through;
END
$f$;

-- migrate:down
DROP FUNCTION IF EXISTS public.metric_trend_announced();
DROP TABLE IF EXISTS public.metric_trend_mark;
DROP TABLE IF EXISTS public.metric_trend_dirty;
DROP TABLE IF EXISTS public.metric_trend;
