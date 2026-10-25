-- migrate:up

-- A calc rule's input may read its key over a time window (#1142, #1065):
-- window_seconds is the window's length, null for an input that reads the
-- key's current values. 1s to an hour, a component rule's alone (the gateway
-- refuses the rest on its field first).
ALTER TABLE public.calc_rule_input ADD COLUMN IF NOT EXISTS window_seconds integer;
ALTER TABLE public.calc_rule_input DROP CONSTRAINT IF EXISTS calc_rule_input_window_seconds_check;
ALTER TABLE public.calc_rule_input ADD CONSTRAINT calc_rule_input_window_seconds_check
    CHECK (window_seconds IS NULL OR (window_seconds >= 1 AND window_seconds <= 3600));

-- When a windowed rule's oldest sample on an owner leaves its window, so the
-- calc tick recomputes it with no new sample: set by the serve that computed
-- the state, null when no windowed input held a sample. The tick marks the
-- owners whose due time passed since that computation.
ALTER TABLE public.calc_rule_state ADD COLUMN IF NOT EXISTS window_due_at timestamp with time zone;
CREATE INDEX IF NOT EXISTS calc_rule_state_window_due_idx ON public.calc_rule_state (window_due_at)
    WHERE window_due_at IS NOT NULL;

-- migrate:down

DROP INDEX IF EXISTS public.calc_rule_state_window_due_idx;
ALTER TABLE public.calc_rule_state DROP COLUMN IF EXISTS window_due_at;
ALTER TABLE public.calc_rule_input DROP CONSTRAINT IF EXISTS calc_rule_input_window_seconds_check;
ALTER TABLE public.calc_rule_input DROP COLUMN IF EXISTS window_seconds;
