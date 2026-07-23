-- migrate:up

-- role joins the universal pattern: the last named entity still keyed by its
-- slug. It gains a uuid id and demotes the slug to a unique, renameable name,
-- exactly as the nine registries did under epic #262. The well-known names
-- (owner / viewer / operator / deploy / admin) stay stable, so the seed, the RBAC
-- engine (which keys by name and expands inherits by name), and the owner
-- invariant keep working on the handle; only the storage identity gains a uuid.
-- The one inbound reference, principal_grant.role_id, moves to the uuid, and the
-- owner-guard trigger resolves 'owner' by name instead of holding the slug.

-- Drop the objects that read role_id before repointing it: the owner-guard
-- trigger + function, the FK, and the two grant unique indexes.
drop trigger if exists principal_grant_owner_guard on principal_grant;
drop function if exists assert_owner_grant_exists();
alter table principal_grant drop constraint principal_grant_role_id_fkey;
drop index if exists principal_grant_unique;
drop index if exists principal_group_grant_unique;

-- role: the slug id becomes name; a fresh uuid becomes the primary key.
alter table role drop constraint role_pkey;
alter table role rename column id to name;
alter table role add column id uuid not null default uuidv7();
alter table role add constraint role_pkey primary key (id);
alter table role add constraint role_name_key unique (name);

-- principal_grant.role_id: slug -> uuid, backfilled by name.
alter table principal_grant add column role_uuid uuid;
update principal_grant g set role_uuid = r.id from role r where r.name = g.role_id;
alter table principal_grant drop column role_id;
alter table principal_grant rename column role_uuid to role_id;
alter table principal_grant alter column role_id set not null;
alter table principal_grant add constraint principal_grant_role_id_fkey
    foreign key (role_id) references role (id);

-- Recreate the grant unique indexes on the repointed column.
create unique index principal_grant_unique
    on principal_grant (principal_id, role_id, scope_kind, coalesce(scope_id, ''), scope_op);
create unique index principal_group_grant_unique
    on principal_grant (group_id, role_id, scope_kind, coalesce(scope_id, ''), scope_op)
    where group_id is not null;

-- Recreate the owner invariant, resolving 'owner' by name (the trigger held the
-- slug literal before). Same deferrable-constraint-trigger shape and OG001 code.
create or replace function assert_owner_grant_exists() returns trigger
    language plpgsql as $$
begin
    if not exists (
        select 1 from principal_grant
        where role_id = (select id from role where name = 'owner') and scope_kind = 'all'
    ) then
        raise exception 'at least one owner grant must remain'
            using errcode = 'OG001';
    end if;
    return null;
end;
$$;

create constraint trigger principal_grant_owner_guard
    after delete or update on principal_grant
    deferrable initially deferred
    for each row
    execute function assert_owner_grant_exists();

-- migrate:down
drop trigger if exists principal_grant_owner_guard on principal_grant;
drop function if exists assert_owner_grant_exists();

drop index if exists principal_group_grant_unique;
drop index if exists principal_grant_unique;
alter table principal_grant drop constraint principal_grant_role_id_fkey;

alter table principal_grant add column role_slug text;
update principal_grant g set role_slug = r.name from role r where r.id = g.role_id;
alter table principal_grant drop column role_id;
alter table principal_grant rename column role_slug to role_id;
alter table principal_grant alter column role_id set not null;

alter table role drop constraint role_name_key;
alter table role drop constraint role_pkey;
alter table role drop column id;
alter table role rename column name to id;
alter table role add constraint role_pkey primary key (id);

alter table principal_grant add constraint principal_grant_role_id_fkey
    foreign key (role_id) references role (id);
create unique index principal_grant_unique
    on principal_grant (principal_id, role_id, scope_kind, coalesce(scope_id, ''), scope_op);
create unique index principal_group_grant_unique
    on principal_grant (group_id, role_id, scope_kind, coalesce(scope_id, ''), scope_op)
    where group_id is not null;

create or replace function assert_owner_grant_exists() returns trigger
    language plpgsql as $$
begin
    if not exists (
        select 1 from principal_grant where role_id = 'owner' and scope_kind = 'all'
    ) then
        raise exception 'at least one owner grant must remain'
            using errcode = 'OG001';
    end if;
    return null;
end;
$$;

create constraint trigger principal_grant_owner_guard
    after delete or update on principal_grant
    deferrable initially deferred
    for each row
    execute function assert_owner_grant_exists();
