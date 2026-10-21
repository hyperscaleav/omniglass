-- migrate:up

-- The calc rule (#995, epic #525; ADR-0144 ruling 1: a sibling table to
-- event_rule): authored config that derives one output key on an owner (a
-- component or a system) from named inputs. inputs is a list of {as, key}
-- (the key a property or metric, by name, as event_rule.sample_key holds it),
-- reduce is a named reducer {reducer} or an expression {expr} over the inputs
-- by their as names. The output key is referenced by id in the registry of
-- its lane, recorded in output_lane, so a key a rule writes cannot be
-- deleted. version bumps on every update. The tick that runs the rules
-- arrives with #525's second slice; this table is the registry it reads.
CREATE TABLE IF NOT EXISTS public.calc_rule (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    name text NOT NULL CONSTRAINT calc_rule_name_key UNIQUE,
    label text,
    description text NOT NULL DEFAULT '',
    owner_kind text NOT NULL CONSTRAINT calc_rule_owner_kind_check CHECK (owner_kind IN ('component', 'system')),
    selector jsonb NOT NULL DEFAULT '{}'::jsonb,
    inputs jsonb NOT NULL CONSTRAINT calc_rule_inputs_check
        CHECK (jsonb_typeof(inputs) = 'array' AND jsonb_array_length(inputs) > 0),
    reduce jsonb NOT NULL CONSTRAINT calc_rule_reduce_check CHECK (jsonb_typeof(reduce) = 'object'),
    output_lane text NOT NULL CONSTRAINT calc_rule_output_lane_check CHECK (output_lane IN ('property', 'metric')),
    output_property_type_id uuid CONSTRAINT calc_rule_output_property_type_id_fkey REFERENCES public.property_type(id),
    output_metric_type_id uuid CONSTRAINT calc_rule_output_metric_type_id_fkey REFERENCES public.metric_type(id),
    enabled boolean NOT NULL DEFAULT true,
    official boolean NOT NULL DEFAULT false,
    version integer NOT NULL DEFAULT 1,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT calc_rule_output_check CHECK (
        (output_lane = 'property' AND output_property_type_id IS NOT NULL AND output_metric_type_id IS NULL)
        OR (output_lane = 'metric' AND output_metric_type_id IS NOT NULL AND output_property_type_id IS NULL))
);

-- The rules reading a key, found by containment (inputs @> '[{"key": "..."}]'):
-- the sample-write path marks them pending in the second slice.
CREATE INDEX IF NOT EXISTS calc_rule_inputs_idx ON public.calc_rule USING gin (inputs jsonb_path_ops) WHERE enabled;

-- migrate:down

DROP TABLE IF EXISTS public.calc_rule;
