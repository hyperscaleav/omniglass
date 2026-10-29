-- migrate:up
-- #1276: the trend tier. One row per observed metric series per hour, holding
-- what a chart needs to draw the hour honestly at any zoom (count, min, max,
-- sum, first, last), written by the server's compaction once the hour is past
-- the late-data horizon, and kept for retention.trend_days after the hour's
-- samples leave the hot tier. ADR-1276.
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

-- A (series, hour) the compaction has already closed that a late sample landed
-- in since: set in the sample's own insert, read and cleared by the next
-- compaction, which recomputes the hour.
CREATE TABLE IF NOT EXISTS public.metric_trend_dirty (
    series_id bigint NOT NULL,
    hour timestamp with time zone NOT NULL,
    CONSTRAINT metric_trend_dirty_pkey PRIMARY KEY (series_id, hour)
);

-- How far the compaction has closed hours: every hour before compacted_through
-- is closed. One row, written by the compaction (absent until its first pass).
CREATE TABLE IF NOT EXISTS public.metric_trend_mark (
    id boolean DEFAULT true NOT NULL,
    compacted_through timestamp with time zone NOT NULL,
    CONSTRAINT metric_trend_mark_pkey PRIMARY KEY (id),
    CONSTRAINT metric_trend_mark_one_row CHECK (id)
);

-- migrate:down
DROP TABLE IF EXISTS public.metric_trend_mark;
DROP TABLE IF EXISTS public.metric_trend_dirty;
DROP TABLE IF EXISTS public.metric_trend;
