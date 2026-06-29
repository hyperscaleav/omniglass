-- migrate:up

-- The component tier: the component_type registry (the shape-definer, mirroring
-- location_type / system_type) and the component tree. A component is a leaf of
-- the estate: it belongs to a system (its primary system), is located at a
-- location, classified by a type, and nestable into sub-components. DDL is
-- idempotent; official component types are seeded at boot.

create table if not exists component_type (
    id           text        primary key,
    official     boolean     not null default false,
    display_name text        not null,
    rank         integer     not null default 0,
    created_at   timestamptz not null default now()
);

-- A component is name-addressable (globally unique), classified by
-- component_type (FK), nestable via parent_id (on delete restrict), and
-- references its primary system and location. system_id / location_id are on
-- delete restrict: a system or location cannot be removed while components are
-- bound to it (the "refused while occupied" rule across the tiers).
create table if not exists component (
    id             uuid        primary key default uuidv7(),
    name           text        not null unique,
    display_name   text,
    component_type text        not null references component_type (id),
    parent_id      uuid        references component (id) on delete restrict,
    system_id      uuid        references system (id) on delete restrict,
    location_id    uuid        references location (id) on delete restrict,
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now()
);
create index if not exists component_parent_idx on component (parent_id);
create index if not exists component_system_idx on component (system_id);
create index if not exists component_location_idx on component (location_id);

-- migrate:down

drop table if exists component;
drop table if exists component_type;
