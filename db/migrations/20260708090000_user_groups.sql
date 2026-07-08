-- migrate:up

-- Principal groups: a group holds role@scope grants that its members inherit, so
-- access is assigned to a team once instead of per user. Static membership, and no
-- nesting (a group does not contain groups). A member's effective grants are its
-- own direct grants unioned with the grants of every group it belongs to; the
-- model is already additive across grants, so nothing downstream changes shape.
create table if not exists principal_group (
    id           uuid        primary key default uuidv7(),
    name         text        not null unique,
    display_name text,
    description  text,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);

-- Membership is a plain join, keyed by the pair so a principal joins a group at
-- most once. Both sides cascade: removing a group or a principal drops the edge.
create table if not exists principal_group_member (
    group_id     uuid        not null references principal_group (id) on delete cascade,
    principal_id uuid        not null references principal (id) on delete cascade,
    created_at   timestamptz not null default now(),
    primary key (group_id, principal_id)
);

-- A grant now targets either a principal or a group, never both. principal_id
-- becomes nullable, group_id is added, and exactly one of the two must be set.
-- Existing rows keep their principal_id and satisfy the constraint unchanged.
alter table principal_grant add column if not exists group_id uuid references principal_group (id) on delete cascade;
alter table principal_grant alter column principal_id drop not null;
alter table principal_grant drop constraint if exists principal_grant_target_ck;
alter table principal_grant add constraint principal_grant_target_ck
    check (num_nonnulls(principal_id, group_id) = 1);

-- Dedup group grants the way principal grants are deduped. The existing
-- principal_grant_unique index keys on principal_id, which is null for a group
-- grant (and nulls are distinct in a unique index), so it never dedups those; a
-- partial index keyed on group_id does.
create unique index if not exists principal_group_grant_unique
    on principal_grant (group_id, role_id, scope_kind, coalesce(scope_id, ''), scope_op)
    where group_id is not null;

-- migrate:down
drop index if exists principal_group_grant_unique;
alter table principal_grant drop constraint if exists principal_grant_target_ck;
-- Best-effort reversal (dev only): dropping group_id discards any group grants;
-- principal_id cannot be safely restored to NOT NULL if a group grant ever
-- existed, so it is left nullable.
alter table principal_grant drop column if exists group_id;
drop table if exists principal_group_member;
drop table if exists principal_group;
