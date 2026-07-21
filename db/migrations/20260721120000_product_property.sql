-- migrate:up

-- product_property is the product's declared-property CONTRACT: which properties a
-- product exposes and their defaults. It replaces field_definition (which hung the
-- field catalog off component_type); the property catalog owns data_type and
-- validation, so neither is duplicated here. Forward-compatible with the driver
-- getter/setter contract (an access/mode column lands with the driver slice).
create table if not exists product_property (
    id            uuid        primary key default uuidv7(),
    product_id    text        not null references product (id) on delete cascade,
    property_name text        not null references property (name) on delete cascade,
    default_value jsonb,
    required      boolean     not null default false,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now(),
    unique (product_id, property_name)
);
create index if not exists product_property_property_idx on product_property (property_name);

-- property_value is the value store: the current value of a property on an estate
-- owner, per provenance. It carries the SAME owner exclusive-arc as metric_datapoint
-- and event (primitive-first), so a value is owned and addressed identically to the
-- samples and occurrences beside it. A declared value is what used to be a field_value;
-- intended (config), observed, and calculated producers land in later slices.
create table if not exists property_value (
    id            uuid        primary key default uuidv7(),
    owner_kind    text        not null,
    component_id  text        references component (name) on delete cascade,
    system_id     text        references system (name) on delete cascade,
    location_id   text        references location (name) on delete cascade,
    node_id       text        references node (name) on delete cascade,
    property_name text        not null references property (name) on delete cascade,
    instance      text        not null default '',
    provenance    text        not null default 'declared',
    value         jsonb       not null,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now(),
    constraint property_value_owner_kind_check check (owner_kind in ('component', 'system', 'location', 'node')),
    constraint property_value_provenance_check check (provenance in ('observed', 'calculated', 'intended', 'declared')),
    constraint property_value_owner_arc_check check (
           (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
        or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
        or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
        or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
    ),
    -- One current value per (owner, property, instance, provenance). NULLS NOT DISTINCT
    -- is required: the owner arc leaves three of the four owner columns NULL, and under
    -- the default NULLS DISTINCT those NULLs would make every row unique, so duplicates
    -- would slip through.
    constraint property_value_series_key unique nulls not distinct
        (owner_kind, component_id, system_id, location_id, node_id, property_name, instance, provenance)
);
create index if not exists property_value_component_idx on property_value (component_id, property_name) where component_id is not null;

-- migrate:down

drop table if exists property_value;
drop table if exists product_property;
