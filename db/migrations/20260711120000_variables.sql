-- migrate:up

-- The variable primitive: a typed, cascade-resolved free value (a macro), owned
-- on the exclusive arc and resolved most-specific-wins down the cascade like a
-- secret, but plaintext (no encryption, no masking). One table: the variable cell
-- itself. Typing is inline (value_type enum + a jsonb value validated in the app),
-- not a registry, matching the "operator-defined, not curated" naming model. DDL
-- is idempotent.
create table if not exists variable (
    id           uuid        primary key default uuidv7(),
    name         text        not null,
    value_type   text        not null check (value_type in ('string', 'int', 'float', 'bool', 'json')),
    owner_kind   text        not null check (owner_kind in ('global', 'component', 'system', 'location')),
    component_id uuid        references component (id) on delete cascade,
    system_id    uuid        references system (id) on delete cascade,
    location_id  uuid        references location (id) on delete cascade,
    value        jsonb       not null,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    -- The exclusive arc: exactly the matching id is set for a scoped owner, and
    -- all three are null for the global singleton.
    constraint variable_owner_arc check (
        (owner_kind = 'global'    and component_id is null     and system_id is null     and location_id is null) or
        (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
        (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
        (owner_kind = 'location'  and location_id is not null   and component_id is null  and system_id is null)
    )
);

-- A variable name is unique per owner, so the cascade resolves at most one value
-- per (name, owner). The same name at different scopes is the cascade, not a
-- clash; these partial uniques enforce one-per-owner without colliding across
-- scopes (nulls would otherwise read as distinct).
create unique index if not exists variable_global_name   on variable (name)               where owner_kind = 'global';
create unique index if not exists variable_component_name on variable (name, component_id) where owner_kind = 'component';
create unique index if not exists variable_system_name    on variable (name, system_id)    where owner_kind = 'system';
create unique index if not exists variable_location_name  on variable (name, location_id)  where owner_kind = 'location';

-- Resolution walks by owner id; index each arc for the cascade lookup.
create index if not exists variable_component_idx on variable (component_id);
create index if not exists variable_system_idx    on variable (system_id);
create index if not exists variable_location_idx  on variable (location_id);
create index if not exists variable_name_idx      on variable (name);

-- migrate:down

drop table if exists variable;
