-- migrate:up

-- A dirtied hour whose late sample the block merge skipped (#1560). The merge
-- covers a late sample only while its day is intact, and the trend recompute
-- reads an hour from its day's block only while the day is; the two run in
-- different transactions, so a window an operator widens between them makes
-- the day intact for a recompute whose sample the merge left out. The merge
-- marks the hour raw_only in the transaction that lands the sample, and the
-- recompute reads such an hour from its raw rows whatever the window is by then.
ALTER TABLE public.metric_trend_dirty ADD COLUMN IF NOT EXISTS raw_only boolean DEFAULT false NOT NULL;

-- migrate:down

ALTER TABLE public.metric_trend_dirty DROP COLUMN IF EXISTS raw_only;
