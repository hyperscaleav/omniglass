-- migrate:up

-- The system tier of the estate: the system_type registry (the shape-definer,
-- mirroring location_type) and the system tree. A system is a composition of
-- components (the service tree), located at a location, classified by a type,
-- and nestable into subsystems. DDL is idempotent; official system types are
-- seeded at boot, not here, per the three-bucket rule.

create table if not exists system_type (
    id           text        primary key,
    official     boolean     not null default false,
    display_name text        not null,
    rank         integer     not null default 0,
    created_at   timestamptz not null default now()
);

-- A system is name-addressable (globally unique), classified by system_type
-- (the FK validates it), nestable via parent_id (subsystem tree, on delete
-- restrict so a parent cannot drop while it has subsystems), and located at an
-- optional location. location_id is on delete restrict: a location cannot be
-- removed while systems are placed in it (the "refused while occupied" rule
-- extends across the tier).
create table if not exists system (
    id            uuid        primary key default uuidv7(),
    name          text        not null unique,
    display_name  text,
    system_type   text        not null references system_type (id),
    parent_id     uuid        references system (id) on delete restrict,
    location_id   uuid        references location (id) on delete restrict,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);
create index if not exists system_parent_idx on system (parent_id);
create index if not exists system_location_idx on system (location_id);

-- migrate:down

drop table if exists system;
drop table if exists system_type;
