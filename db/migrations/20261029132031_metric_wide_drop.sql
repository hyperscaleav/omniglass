-- migrate:up
-- #1275, last of three: the swap's end. Every wide metric row now lives in the
-- narrow lane (the backfill verified the count), so the wide table goes.
DROP TABLE IF EXISTS public.metric_wide;

-- migrate:down
-- The wide table as it stood before the narrow lane, empty, under the names the
-- first migration of the three gave it; the backfill's down refills it and the
-- first one's down renames it back to metric.
CREATE TABLE IF NOT EXISTS public.metric_wide (
    id bigint GENERATED ALWAYS AS IDENTITY,
    ts timestamp with time zone DEFAULT now() NOT NULL,
    owner_kind text NOT NULL,
    instance text DEFAULT ''::text NOT NULL,
    value double precision NOT NULL,
    provenance text DEFAULT 'observed'::text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    source_rule text,
    source_rule_version bigint,
    event_id bigint,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    metric_type_id uuid CONSTRAINT metric_property_type_id_not_null NOT NULL,
    command_id bigint,
    source_rule_id uuid,
    CONSTRAINT metric_lineage_check CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL) AND (command_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL) AND (command_id IS NULL)) OR ((provenance = 'intended'::text) AND (command_id IS NOT NULL) AND (source_rule IS NULL)))),
    CONSTRAINT metric_owner_arc_check CHECK ((((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL) AND (location_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'location'::text) AND (location_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (node_id IS NULL)) OR ((owner_kind = 'node'::text) AND (node_id IS NOT NULL) AND (component_id IS NULL) AND (system_id IS NULL) AND (location_id IS NULL)))),
    CONSTRAINT metric_owner_kind_check CHECK ((owner_kind = ANY (ARRAY['component'::text, 'system'::text, 'location'::text, 'node'::text]))),
    CONSTRAINT metric_provenance_check CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text]))),
    CONSTRAINT metric_wide_pkey PRIMARY KEY (id),
    CONSTRAINT metric_command_id_fkey FOREIGN KEY (command_id) REFERENCES public.command(id),
    CONSTRAINT metric_component_id_fkey FOREIGN KEY (component_id) REFERENCES public.component(id) ON DELETE CASCADE,
    CONSTRAINT metric_event_id_fkey FOREIGN KEY (event_id) REFERENCES public.event(id) ON DELETE SET NULL,
    CONSTRAINT metric_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.location(id) ON DELETE CASCADE,
    CONSTRAINT metric_metric_type_id_fkey FOREIGN KEY (metric_type_id) REFERENCES public.metric_type(id) ON DELETE CASCADE,
    CONSTRAINT metric_node_id_fkey FOREIGN KEY (node_id) REFERENCES public.node(principal_id) ON DELETE CASCADE,
    CONSTRAINT metric_system_id_fkey FOREIGN KEY (system_id) REFERENCES public.system(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS metric_wide_arc_owner_idx ON public.metric_wide USING btree (metric_type_id, COALESCE(system_id, location_id, node_id), instance, ts DESC) WHERE (component_id IS NULL);
CREATE INDEX IF NOT EXISTS metric_wide_owner_idx ON public.metric_wide USING btree (component_id, metric_type_id, instance, ts DESC) WHERE (component_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS metric_wide_ts_brin ON public.metric_wide USING brin (ts);
