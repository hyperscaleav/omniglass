-- migrate:up
-- An event rule's sample window (#1985, ADR-1985): how far back over its
-- key's stored history the rule's criteria read, bound as window, so a rule
-- fires on a sustained trend (slope, growth, span) rather than on one value.
-- 0 is a rule that reads only the sample. Bounded at a week, the longest
-- window a rule reads (rules.MaxSampleWindow).
ALTER TABLE public.event_rule ADD COLUMN IF NOT EXISTS sample_window interval NOT NULL DEFAULT '0';
ALTER TABLE public.event_rule DROP CONSTRAINT IF EXISTS event_rule_sample_window_check;
ALTER TABLE public.event_rule ADD CONSTRAINT event_rule_sample_window_check
    CHECK (sample_window >= '0' AND sample_window <= '7 days');

-- migrate:down
ALTER TABLE public.event_rule DROP CONSTRAINT IF EXISTS event_rule_sample_window_check;
ALTER TABLE public.event_rule DROP COLUMN IF EXISTS sample_window;
