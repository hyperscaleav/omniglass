-- migrate:up

-- The tag primitive: an operator key: value label. Two tables, mirroring the
-- vocabulary-plus-cell split. The `tag` registry is the tenant-wide governed key
-- vocabulary (minting a key needs tag:create); `tag_binding` sets a value for a
-- key at a scope on the exclusive arc and resolves union-on-key,
-- override-on-value down the cascade like a variable. DDL is idempotent.

-- The key vocabulary. name is the normalized key (validated lowercase in the
-- app), unique tenant-wide (one registry per database, the tenant boundary).
-- applies_to narrows a key to a subset of entity kinds (empty = universal);
-- propagates says whether a bound value cascades to descendants (true) or binds
-- as a flat per-entity set that only resolves on its exact owner (false).
create table if not exists tag (
    id          uuid        primary key default uuidv7(),
    name        text        not null unique,
    applies_to  text[]      not null default '{}',
    propagates  boolean     not null default true,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);

create index if not exists tag_name_idx on tag (name);

-- The binding cell: a value for a key at one owner on the exclusive arc. Exactly
-- the matching id is set for a scoped owner, and all three are null for the
-- global singleton, the same arc as variable and secret.
create table if not exists tag_binding (
    id           uuid        primary key default uuidv7(),
    tag_id       uuid        not null references tag (id) on delete cascade,
    owner_kind   text        not null check (owner_kind in ('global', 'component', 'system', 'location')),
    component_id uuid        references component (id) on delete cascade,
    system_id    uuid        references system (id) on delete cascade,
    location_id  uuid        references location (id) on delete cascade,
    value        text        not null,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    constraint tag_binding_owner_arc check (
        (owner_kind = 'global'    and component_id is null     and system_id is null     and location_id is null) or
        (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
        (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
        (owner_kind = 'location'  and location_id is not null   and component_id is null  and system_id is null)
    )
);

-- A key binds at most one value per owner, so the cascade resolves at most one
-- binding per (key, owner). The same key at different scopes is the cascade, not
-- a clash; these partial uniques enforce one-per-owner without colliding across
-- scopes (nulls would otherwise read as distinct).
create unique index if not exists tag_binding_global_key    on tag_binding (tag_id)               where owner_kind = 'global';
create unique index if not exists tag_binding_component_key on tag_binding (tag_id, component_id) where owner_kind = 'component';
create unique index if not exists tag_binding_system_key    on tag_binding (tag_id, system_id)    where owner_kind = 'system';
create unique index if not exists tag_binding_location_key  on tag_binding (tag_id, location_id)  where owner_kind = 'location';

-- Resolution walks by owner id; index each arc for the cascade lookup.
create index if not exists tag_binding_tag_idx       on tag_binding (tag_id);
create index if not exists tag_binding_component_idx on tag_binding (component_id);
create index if not exists tag_binding_system_idx    on tag_binding (system_id);
create index if not exists tag_binding_location_idx  on tag_binding (location_id);

-- migrate:down

drop table if exists tag_binding;
drop table if exists tag;
