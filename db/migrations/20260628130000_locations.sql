-- migrate:up

-- The location estate model: the location_type registry (the only shape-definer
-- for a location, since locations have no template) and the location tree itself.
-- Surrogate keys are uuidv7 where the row is operator data; the type registry is
-- keyed by a stable text id like role. DDL is idempotent. The official location
-- types are seeded at boot (idempotent upsert), not here, per the three-bucket
-- rule.

-- A location_type classifies a location. Shaped like role: a stable text id, an
-- official flag, a display_name, and a rank. rank orders types and gives a soft
-- hierarchy signal (campus below building below floor below room); it does not
-- constrain nesting, which stays free-form on the location tree. official types
-- ship with the binary; operator-defined types via the namespace shadow are a
-- later slice.
create table if not exists location_type (
    id           text        primary key,
    official     boolean     not null default false,
    display_name text        not null,
    rank         integer     not null default 0,
    created_at   timestamptz not null default now()
);

-- A location ties the estate to a physical place. It is a variable-depth tree via
-- parent_id (self-reference), name-addressable (name is globally unique), and
-- classified by location_type (the FK is the validation that the type exists).
-- parent_id is on delete restrict so a parent cannot be dropped while it still
-- has child locations: the "refused while occupied" rule, enforced for the
-- structural children at the schema, with placed systems/components added when
-- those entities land.
create table if not exists location (
    id            uuid        primary key default uuidv7(),
    name          text        not null unique,
    display_name  text,
    location_type text        not null references location_type (id),
    parent_id     uuid        references location (id) on delete restrict,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);
create index if not exists location_parent_idx on location (parent_id);

-- migrate:down

drop table if exists location;
drop table if exists location_type;
