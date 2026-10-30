-- migrate:up

-- Identity bindings (#1385, ADR-1282). A conduit component (one that speaks
-- for many owners, a cloud service or a controller fronting many components)
-- reports values that each name an identity: a match key, a registered
-- property type, and its value. A binding ties one (conduit, key, value) to
-- exactly one owner, a system or a component, on the exclusive owner arc. A
-- location or a node owns no observed sample (ADR-0422), so neither is an arc
-- arm here. Deleting the conduit or the owner deletes the binding; a match
-- key a binding still uses cannot be deleted from under it.
CREATE TABLE IF NOT EXISTS public.identity_binding (
    id uuid DEFAULT uuidv7() NOT NULL PRIMARY KEY,
    conduit_id uuid NOT NULL REFERENCES public.component(id) ON DELETE CASCADE,
    property_type_id uuid NOT NULL REFERENCES public.property_type(id) ON DELETE RESTRICT,
    value text NOT NULL,
    owner_kind text NOT NULL,
    component_id uuid REFERENCES public.component(id) ON DELETE CASCADE,
    system_id uuid REFERENCES public.system(id) ON DELETE CASCADE,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT identity_binding_value_check CHECK (char_length(value) BETWEEN 1 AND 256),
    CONSTRAINT identity_binding_owner_kind_check CHECK (owner_kind = ANY (ARRAY['component'::text, 'system'::text])),
    CONSTRAINT identity_binding_owner_arc CHECK (
        ((owner_kind = 'component'::text) AND (component_id IS NOT NULL) AND (system_id IS NULL))
        OR ((owner_kind = 'system'::text) AND (system_id IS NOT NULL) AND (component_id IS NULL))),
    CONSTRAINT identity_binding_identity_key UNIQUE (conduit_id, property_type_id, value)
);

-- The owner ends' cascades walk these; the conduit end rides the unique key.
CREATE INDEX IF NOT EXISTS identity_binding_component_idx ON public.identity_binding (component_id)
    WHERE component_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS identity_binding_system_idx ON public.identity_binding (system_id)
    WHERE system_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS identity_binding_property_type_idx ON public.identity_binding (property_type_id);

-- migrate:down

DROP TABLE IF EXISTS public.identity_binding;
