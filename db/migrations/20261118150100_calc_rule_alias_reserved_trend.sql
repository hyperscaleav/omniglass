-- migrate:up

-- One-time data backfill (#1985): slope, growth and span are now list
-- functions, so the calc site reserves them (rules.CalcReservedNames) and an
-- alias may no longer take one. A rule holding such an alias is disabled, as
-- 20261022050000_calc_rule_alias_reserved disabled the names reserved then,
-- and reads back with a calc_error on that alias until it is renamed.
-- Idempotent.
UPDATE public.calc_rule c SET enabled = false
WHERE c.enabled AND EXISTS (
    SELECT 1 FROM public.calc_rule_input i
    WHERE i.rule_id = c.id AND i.alias = ANY (ARRAY['growth', 'slope', 'span']::text[]));

-- migrate:down

-- A data backfill is not reversed: re-enabling a rule is an operator's act.
SELECT 1;
