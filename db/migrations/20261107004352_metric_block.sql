-- migrate:up
-- #1528: the compacted tier's blocks (ADR-1528). One row holds one observed
-- metric series' samples over one span, an hour, a UTC day or an ISO week
-- (Monday start), as millisecond offsets from span_start and their values, in
-- landing order (ts, then id), so duplicate instants round-trip (ADR-0061).
-- The span is chosen by the series-day's own sample count, so a block crosses
-- Postgres's 2 KB compression threshold. No summary columns: an hour's
-- aggregates are metric_trend's (ADR-1276). Written beside the raw rows, which
-- stay authoritative until reads over blocks land.
CREATE TABLE IF NOT EXISTS public.metric_block (
    series_id bigint NOT NULL REFERENCES public.metric_series(id) ON DELETE CASCADE,
    span_start timestamp with time zone NOT NULL,
    span text NOT NULL,
    n integer NOT NULL,
    offset_ms integer[] NOT NULL,
    value double precision[] NOT NULL,
    CONSTRAINT metric_block_pkey PRIMARY KEY (series_id, span_start, span),
    CONSTRAINT metric_block_span_check CHECK (span = ANY (ARRAY['hour'::text, 'day'::text, 'week'::text])),
    CONSTRAINT metric_block_n_check CHECK (n > 0 AND cardinality(offset_ms) = n AND cardinality(value) = n)
);

-- A (series, UTC day) a sample landed in after the trend compaction had
-- announced its hour: set in the sample's own insert, beside the trend's dirty
-- mark, and read and cleared by a block pass once the day is folded, which
-- rebuilds the day's blocks (and its week's) from the raw rows.
CREATE TABLE IF NOT EXISTS public.metric_block_dirty (
    series_id bigint NOT NULL,
    day timestamp with time zone NOT NULL,
    CONSTRAINT metric_block_dirty_pkey PRIMARY KEY (series_id, day)
);

-- How far the block folds have gone, as two cursors. The day fold has folded
-- every series-day before blocked_through, and the series of the day at
-- blocked_through up to blocked_series by id; the week fold, every week before
-- week_through, and the series of the week at week_through up to week_series.
-- Each moves in the transaction that writes the blocks it passes. One row,
-- written by the block pass (absent until its first).
CREATE TABLE IF NOT EXISTS public.metric_block_mark (
    id boolean DEFAULT true NOT NULL,
    blocked_through timestamp with time zone NOT NULL,
    blocked_series bigint DEFAULT 0 NOT NULL,
    week_through timestamp with time zone NOT NULL,
    week_series bigint DEFAULT 0 NOT NULL,
    CONSTRAINT metric_block_mark_pkey PRIMARY KEY (id),
    CONSTRAINT metric_block_mark_one_row CHECK (id),
    CONSTRAINT metric_block_mark_order CHECK (week_through <= blocked_through)
);

-- migrate:down
DROP TABLE IF EXISTS public.metric_block_mark;
DROP TABLE IF EXISTS public.metric_block_dirty;
DROP TABLE IF EXISTS public.metric_block;
