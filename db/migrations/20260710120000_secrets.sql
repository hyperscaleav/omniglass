-- migrate:up

-- The secret primitive: a sensitive operator-set value, resolved down the
-- cascade and encrypted at rest. Two tables: the secret_type shape registry
-- (mirroring component_type / system_type: id text PK, official boolean, plus
-- the per-field schema) and the secret cell itself, owned on the exclusive arc
-- and cascaded. DDL is idempotent; official shapes are seeded at boot.

-- secret_type: the named shape a secret takes (snmp_community, basic_auth,
-- oauth2, ...). The field schema is a jsonb array of
-- {name, type, secret, origin}, where secret drives encryption + masking and
-- origin is operator | lifecycle (operator fields are set at creation; lifecycle
-- fields are filled by the secret's own machinery later). official marks the
-- ship-with canonical set.
create table if not exists secret_type (
    id           text        primary key,
    official     boolean     not null default false,
    display_name text        not null,
    schema       jsonb       not null default '[]'::jsonb,
    created_at   timestamptz not null default now()
);

-- secret: one cascaded, encrypted value. Owned on the exclusive arc
-- (owner_kind + exactly the matching typed FK, or all-null for the global
-- singleton), classified by secret_type. value holds the field map: a secret
-- field stores its {ciphertext, nonce, wrapped_dek, key_id} envelope, a
-- non-secret field stores its plaintext scalar. The plaintext of a secret field
-- is never stored. Owner FKs are on delete cascade: removing the owning entity
-- removes its secrets.
create table if not exists secret (
    id           uuid        primary key default uuidv7(),
    name         text        not null,
    secret_type  text        not null references secret_type (id),
    owner_kind   text        not null check (owner_kind in ('global', 'component', 'system', 'location')),
    component_id uuid        references component (id) on delete cascade,
    system_id    uuid        references system (id) on delete cascade,
    location_id  uuid        references location (id) on delete cascade,
    value        jsonb       not null default '{}'::jsonb,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    -- The exclusive arc: exactly the matching id is set for a scoped owner, and
    -- all three are null for the global singleton.
    constraint secret_owner_arc check (
        (owner_kind = 'global'    and component_id is null     and system_id is null     and location_id is null) or
        (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
        (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
        (owner_kind = 'location'  and location_id is not null   and component_id is null  and system_id is null)
    )
);

-- A secret name is unique per owner, so the cascade resolves at most one value
-- per (name, owner). The same name at different scopes is the cascade, not a
-- clash; these partial uniques enforce one-per-owner without colliding across
-- scopes (nulls would otherwise read as distinct).
create unique index if not exists secret_global_name   on secret (name)               where owner_kind = 'global';
create unique index if not exists secret_component_name on secret (name, component_id) where owner_kind = 'component';
create unique index if not exists secret_system_name    on secret (name, system_id)    where owner_kind = 'system';
create unique index if not exists secret_location_name  on secret (name, location_id)  where owner_kind = 'location';

-- Resolution walks by owner id; index each arc for the cascade lookup.
create index if not exists secret_component_idx on secret (component_id);
create index if not exists secret_system_idx    on secret (system_id);
create index if not exists secret_location_idx  on secret (location_id);
create index if not exists secret_name_idx      on secret (name);

-- migrate:down

drop table if exists secret;
drop table if exists secret_type;
