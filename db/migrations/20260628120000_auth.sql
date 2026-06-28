-- migrate:up

-- Identity and access foundation: principals (plus per-kind human and service),
-- credentials, roles, grants (role x scope), and the audit log. Surrogate keys
-- are uuidv7 (time-ordered, opaque, app- or DB-generatable), with a unique name
-- where the entity is name-addressable. DDL is idempotent. The four official
-- roles are seeded at boot (idempotent upsert), not here, per the three-bucket
-- rule.

create table if not exists principal (
    id         uuid        primary key default uuidv7(),
    kind       text        not null check (kind in ('human', 'service', 'node', 'agent')),
    created_at timestamptz not null default now()
);

create table if not exists human (
    principal_id uuid primary key references principal (id) on delete cascade,
    username     text not null unique,
    email        text,
    display_name text
);

create table if not exists service (
    principal_id uuid primary key references principal (id) on delete cascade,
    label        text not null
);

-- A credential authenticates a principal. The secret is stored only as its
-- sha256 (bytea); the cleartext token is shown once at mint time. prefix is a
-- non-secret human-readable locator for scanners and audit.
create table if not exists credential (
    id           uuid        primary key default uuidv7(),
    principal_id uuid        not null references principal (id) on delete cascade,
    kind         text        not null check (kind in ('bearer')),
    secret_hash  bytea       not null,
    prefix       text        not null,
    created_at   timestamptz not null default now(),
    last_used_at timestamptz
);
create unique index if not exists credential_secret_hash_key on credential (secret_hash);

-- A role is a named capability set: permissions are <resource>:<action> strings,
-- inherits names parent role ids. official roles ship with the binary.
create table if not exists role (
    id          text        primary key,
    official    boolean     not null default false,
    permissions text[]      not null default '{}',
    inherits    text[]      not null default '{}',
    created_at  timestamptz not null default now()
);

-- A grant pairs a role with a scope on a principal. Permissions are additive
-- across grants. scope_id is null for the all scope.
create table if not exists principal_grant (
    id           uuid        primary key default uuidv7(),
    principal_id uuid        not null references principal (id) on delete cascade,
    role_id      text        not null references role (id),
    scope_kind   text        not null check (scope_kind in ('all', 'location', 'system', 'component', 'group')),
    scope_id     text,
    created_at   timestamptz not null default now()
);
-- Deduplicate grants including the all scope, where scope_id is null; a plain
-- unique constraint would treat two null scope_ids as distinct.
create unique index if not exists principal_grant_unique
    on principal_grant (principal_id, role_id, scope_kind, coalesce(scope_id, ''));

-- The audit log records every write: the resolved actor, the verb, the resource,
-- and the old/new shape, written in the same transaction as the change.
create table if not exists audit_log (
    id                 uuid        primary key default uuidv7(),
    ts                 timestamptz not null default now(),
    actor_principal_id uuid        references principal (id),
    verb               text        not null,
    resource           text        not null,
    resource_id        text,
    old                jsonb,
    new                jsonb
);
create index if not exists audit_log_ts_idx on audit_log (ts);

-- migrate:down

drop table if exists audit_log;
drop table if exists principal_grant;
drop table if exists role;
drop table if exists credential;
drop table if exists service;
drop table if exists human;
drop table if exists principal;
