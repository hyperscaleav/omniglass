-- migrate:up

-- Component dependencies (#1648). An explicit edge from a dependent
-- (component_id) to what it depends on (upstream_id), of one of three kinds:
-- power (fed from an outlet of the upstream, a PDU; the outlet is required on
-- this kind and absent on the others), host (hosted by the upstream, a chassis
-- and its line card, a codec and the peripheral it reboots) and path (reached
-- through the upstream, a gateway). Containment (component.parent_id) is not a
-- dependency and stays apart. Deleting either end deletes the edge. A cycle
-- within a kind is refused at create, in the gateway, since a CHECK cannot see
-- other rows.
CREATE TABLE IF NOT EXISTS public.component_dependency (
    id uuid DEFAULT uuidv7() NOT NULL PRIMARY KEY,
    component_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    upstream_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    kind text NOT NULL,
    outlet text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT component_dependency_kind_check CHECK (kind = ANY (ARRAY['power'::text, 'host'::text, 'path'::text])),
    CONSTRAINT component_dependency_outlet_check CHECK (
        ((kind = 'power'::text) AND (outlet IS NOT NULL) AND (char_length(outlet) BETWEEN 1 AND 64))
        OR ((kind <> 'power'::text) AND (outlet IS NULL))),
    CONSTRAINT component_dependency_self_check CHECK (component_id <> upstream_id),
    CONSTRAINT component_dependency_edge_key UNIQUE (component_id, upstream_id, kind)
);

-- The dependents walk (a hold set, the upstream's side of a list) and the
-- upstream end's cascade read this; the dependent end rides the unique key.
CREATE INDEX IF NOT EXISTS component_dependency_upstream_idx ON public.component_dependency (upstream_id, kind);

-- An out-of-band endpoint (a BMC, a management port) answers through a
-- reboot of its device, so a reboot's hold leaves its gate open. A power
-- command on the outlet that feeds the device still holds it.
ALTER TABLE public.endpoint
    ADD COLUMN IF NOT EXISTS out_of_band boolean DEFAULT false NOT NULL;

-- The power-command marker: the name of the param that carries the outlet.
-- Set only on a disruptive type, since a power command takes down what its
-- outlet feeds.
ALTER TABLE public.command_type
    ADD COLUMN IF NOT EXISTS power_outlet_param text;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'command_type_power_outlet_param_check') THEN
        ALTER TABLE public.command_type ADD CONSTRAINT command_type_power_outlet_param_check CHECK (
            power_outlet_param IS NULL
            OR (disruptive AND char_length(power_outlet_param) BETWEEN 1 AND 64));
    END IF;
END $$;

-- migrate:down

ALTER TABLE public.command_type DROP CONSTRAINT IF EXISTS command_type_power_outlet_param_check;
ALTER TABLE public.command_type DROP COLUMN IF EXISTS power_outlet_param;
ALTER TABLE public.endpoint DROP COLUMN IF EXISTS out_of_band;
DROP TABLE IF EXISTS public.component_dependency;
