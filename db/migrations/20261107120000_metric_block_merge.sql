-- migrate:up
-- #1529: a late observed sample merges into the block that covers its day in
-- its own batch's transaction (ADR-1529), so no block is rebuilt from the raw
-- rows and the dirtied-day marks #1528 kept for the rebuild go.
DROP TABLE IF EXISTS public.metric_block_dirty;

-- migrate:down
CREATE TABLE IF NOT EXISTS public.metric_block_dirty (
    series_id bigint NOT NULL,
    day timestamp with time zone NOT NULL,
    CONSTRAINT metric_block_dirty_pkey PRIMARY KEY (series_id, day)
);
