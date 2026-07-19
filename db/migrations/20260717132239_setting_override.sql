-- migrate:up

-- setting_override: the override layers of the settings engine. Base layers (code
-- defaults and the operator settings file) live in memory and are never stored
-- here; this table holds only what an operator changed, so restoring defaults is a
-- DELETE. scope is the cascade level ('global' now; 'group' and 'user' in a
-- fast-follow), principal_id names the group or user for a non-global row and is
-- NULL for global. Identity is (scope, principal_id, namespace) with NULLS NOT
-- DISTINCT so the NULL principal of a global row counts as one value (one global
-- row per namespace); a surrogate id is the primary key because a nullable column
-- cannot sit in a PK. doc holds the override values for the namespace; locks is the
-- set of locked key-paths at this level. This table is operator data: never
-- boot-seeded. Additive and idempotent.
create table if not exists setting_override (
    id           uuid        primary key default uuidv7(),
    scope        text        not null,
    principal_id uuid,
    namespace    text        not null,
    doc          jsonb       not null default '{}'::jsonb,
    locks        jsonb       not null default '[]'::jsonb,
    updated_at   timestamptz not null default now(),
    updated_by   uuid,
    constraint setting_override_identity unique nulls not distinct (scope, principal_id, namespace)
);

-- migrate:down

drop table if exists setting_override;
