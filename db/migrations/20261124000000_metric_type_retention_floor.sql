-- migrate:up
-- A metric day folds into blocks 48 hours after it starts, so a type's own
-- window under 3 days folds nothing (#2144). The write now refuses it; this
-- raises one stored before the minimum to the minimum.
UPDATE public.metric_type SET retention_days = 3 WHERE retention_days < 3;

-- migrate:down
-- The prior windows are not recoverable and the raise loses nothing.
SELECT 1;
