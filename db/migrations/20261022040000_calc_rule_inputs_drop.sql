-- migrate:up

-- calc_rule.inputs held each input's key by name (#995); its rows now live in
-- calc_rule_input by id (#1018, moved by the backfill before this), so the
-- column and its index go. A reduce is always an expression: {expr}, never a
-- named {reducer} (the backfill rewrote each).
DROP INDEX IF EXISTS public.calc_rule_inputs_idx;
ALTER TABLE public.calc_rule DROP COLUMN IF EXISTS inputs;
ALTER TABLE public.calc_rule DROP CONSTRAINT IF EXISTS calc_rule_reduce_check;
ALTER TABLE public.calc_rule ADD CONSTRAINT calc_rule_reduce_check
    CHECK (jsonb_typeof(reduce) = 'object' AND jsonb_typeof(reduce->'expr') = 'string' AND NOT (reduce ? 'reducer'));

-- migrate:down

ALTER TABLE public.calc_rule DROP CONSTRAINT IF EXISTS calc_rule_reduce_check;
ALTER TABLE public.calc_rule ADD CONSTRAINT calc_rule_reduce_check CHECK (jsonb_typeof(reduce) = 'object');
ALTER TABLE public.calc_rule ADD COLUMN IF NOT EXISTS inputs jsonb NOT NULL DEFAULT '[]'::jsonb;
UPDATE public.calc_rule c SET inputs = coalesce((
    SELECT jsonb_agg(jsonb_build_object('as', i.alias, 'key', coalesce(pt.name, mt.name)) ORDER BY i.position)
    FROM public.calc_rule_input i
    LEFT JOIN public.property_type pt ON pt.id = i.property_type_id
    LEFT JOIN public.metric_type mt ON mt.id = i.metric_type_id
    WHERE i.rule_id = c.id), '[]'::jsonb);
CREATE INDEX IF NOT EXISTS calc_rule_inputs_idx ON public.calc_rule USING gin (inputs jsonb_path_ops) WHERE enabled;
