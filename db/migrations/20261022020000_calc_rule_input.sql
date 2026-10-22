-- migrate:up

-- A calc rule's inputs held by uuid (#1018): one row per input, its alias
-- (the name the rule's expression binds it by) and the key it reads,
-- referenced by id in the registry of its lane, recorded in lane, as the
-- output key already is. A key rename changes nothing the rule holds, and a
-- key a rule reads cannot be deleted from under it. position keeps the
-- authored order. Each alias, and each key, appears once per rule. Replaces
-- calc_rule.inputs, the {as, key} list that held keys by name (moved by the
-- calc_rule_input backfill, dropped after it).
CREATE TABLE IF NOT EXISTS public.calc_rule_input (
    rule_id uuid NOT NULL CONSTRAINT calc_rule_input_rule_id_fkey REFERENCES public.calc_rule(id) ON DELETE CASCADE,
    position integer NOT NULL CONSTRAINT calc_rule_input_position_check CHECK (position >= 0),
    alias text NOT NULL,
    lane text NOT NULL CONSTRAINT calc_rule_input_lane_check CHECK (lane IN ('property', 'metric')),
    property_type_id uuid CONSTRAINT calc_rule_input_property_type_id_fkey REFERENCES public.property_type(id),
    metric_type_id uuid CONSTRAINT calc_rule_input_metric_type_id_fkey REFERENCES public.metric_type(id),
    CONSTRAINT calc_rule_input_pkey PRIMARY KEY (rule_id, position),
    CONSTRAINT calc_rule_input_alias_key UNIQUE (rule_id, alias),
    CONSTRAINT calc_rule_input_key_check CHECK (
        (lane = 'property' AND property_type_id IS NOT NULL AND metric_type_id IS NULL)
        OR (lane = 'metric' AND metric_type_id IS NOT NULL AND property_type_id IS NULL))
);

-- A key is read once per rule.
CREATE UNIQUE INDEX IF NOT EXISTS calc_rule_input_property_key ON public.calc_rule_input (property_type_id, rule_id)
    WHERE property_type_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS calc_rule_input_metric_key ON public.calc_rule_input (metric_type_id, rule_id)
    WHERE metric_type_id IS NOT NULL;

-- migrate:down

DROP TABLE IF EXISTS public.calc_rule_input;
