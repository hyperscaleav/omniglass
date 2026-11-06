-- migrate:up
-- #1526: a metric sample's ts is whole milliseconds, so a raw row and the
-- compacted block that later holds it (int4 millisecond offsets, #1265) agree
-- exactly. NTP-synced node clocks are accurate to a few milliseconds, so the
-- microsecond digits were noise. The node and the ingest truncate what they
-- write from here; this truncates what is stored and the column default.
--
-- A truncation only lowers a ts by under a millisecond, and a partition's
-- bounds are whole milliseconds, so no row leaves its partition. It may leave
-- two samples of a series in one instant, which is allowed (ADR-0061: no
-- unique key on (series_id, ts)); ids still order them as they landed. The
-- declared tier's ts is a command's issue time, the same whole milliseconds.
UPDATE public.metric SET ts = date_trunc('milliseconds', ts) WHERE ts <> date_trunc('milliseconds', ts);
UPDATE public.metric_declared SET ts = date_trunc('milliseconds', ts) WHERE ts <> date_trunc('milliseconds', ts);
ALTER TABLE public.metric ALTER COLUMN ts SET DEFAULT date_trunc('milliseconds', now());
ALTER TABLE public.metric_declared ALTER COLUMN ts SET DEFAULT date_trunc('milliseconds', now());

-- migrate:down
-- The truncated stamps stay as they are (the microseconds are not recoverable);
-- the defaults go back to now().
ALTER TABLE public.metric_declared ALTER COLUMN ts SET DEFAULT now();
ALTER TABLE public.metric ALTER COLUMN ts SET DEFAULT now();
