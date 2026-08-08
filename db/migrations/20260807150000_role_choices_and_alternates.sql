-- migrate:up

-- #626 (task 7): a role can belong to an exclusive-or group instead of
-- contributing to its system unconditionally. role_choice is the group (a
-- standard's "conferencing" need, say); choice_alternate is one way to
-- satisfy it (an "all-in-one" video bar, or a "component-system" build of
-- codec+camera+dsp+amp+mic); system_role.alternate_id (below) is how a role
-- joins one. Both new tables carry the SAME owner arc system_role already
-- has (owner_kind, standard_id, system_id), not just role_choice: without a
-- matching pair on choice_alternate a system-owned ad-hoc role could point
-- at an unrelated standard's alternate and vanish from that system's
-- verdict or double-count elsewhere.
--
-- owner_ref collapses the arc's two nullable columns to one that is never
-- null, and every composite FK below references it instead of standard_id
-- and system_id directly. This is load-bearing, not cosmetic: a foreign key
-- checks under MATCH SIMPLE by default, which skips enforcement of the
-- WHOLE constraint the moment any one of its columns is null on the
-- referencing row. standard_id and system_id always have exactly one null
-- (the arc's own shape), so a composite FK naming both of them is silently
-- never enforced, for every row, regardless of what alternate_id says. This
-- was caught empirically: TestRoleCannotJoinForeignAlternate passed a role
-- under one standard an alternate id from a different one and nothing
-- refused it, against the four-column form. owner_ref is the fix: it is
-- always non-null (exactly one of standard_id/system_id always is, by
-- role_choice_owner_arc_check and system_role_owner_arc_check below), so
-- the only nullable column left in a composite FK is alternate_id itself,
-- which is exactly the one column NULL is supposed to mean something for
-- (unconditional, see the comment on system_role_alternate_fk).
--
-- Unlike system_role (repaired further down), the owner arc CHECK is here
-- from the table's first migration: both new tables are new, so there is no
-- legacy data to raise-and-report over.
create table if not exists role_choice (
    id uuid primary key default uuidv7(),
    owner_kind text not null,
    standard_id uuid references standard(id) on delete cascade,
    system_id uuid references system(id) on delete cascade,
    owner_ref uuid generated always as (coalesce(standard_id, system_id)) stored,
    name text not null,
    display_name text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint role_choice_owner_kind_check check (owner_kind in ('standard', 'system')),
    constraint role_choice_owner_arc_check check (
      (owner_kind = 'standard' and standard_id is not null and system_id is null) or
      (owner_kind = 'system'   and system_id is not null and standard_id is null)),
    -- owner_ref is never null (the CHECK above guarantees it), so this is a
    -- plain UNIQUE: no NULLS NOT DISTINCT dance, unlike system_role_name_key,
    -- because there is no second arc column left to collide on null.
    constraint role_choice_name_key unique (owner_kind, owner_ref, name),
    -- The FK target for choice_alternate_owner_fk below: id alone is
    -- already unique (the primary key), but a composite foreign key needs
    -- an explicit unique constraint on the exact column list it
    -- references, so this restates it rather than relying on the PK.
    constraint role_choice_owner_key unique (id, owner_kind, owner_ref)
);

-- choice_alternate.position orders the alternates within a choice for the
-- tie-break rule (internal/health.Choice.Active): nothing else in the
-- schema orders anything by an explicit ordinal, so this is the first one.
-- owner_kind, standard_id and system_id are denormalised from the owning
-- choice, written by the gateway from the choice an alternate is created
-- under and never caller-supplied: that is what lets
-- system_role_alternate_fk below refuse a role that names an alternate
-- outside its own owner arc.
--
-- choice_alternate_position_key is DEFERRABLE INITIALLY IMMEDIATE, the same
-- reason system_role_assignment_position_key already is
-- (20260807146000_assignment_position_floor.sql): SeedRoleChoice converges
-- every alternate on its declared position on every boot, existing rows
-- included, which is a genuine reassignment (not just an insert), so two
-- alternates can pass through a duplicate position mid-transaction on the
-- way to their final, non-colliding permutation. A plain UNIQUE index is
-- checked per updated row and would raise on the first move; deferring to
-- end-of-transaction lets the whole reconciliation land before anything is
-- checked.
create table if not exists choice_alternate (
    id uuid primary key default uuidv7(),
    choice_id uuid not null references role_choice(id) on delete cascade,
    owner_kind text not null,
    standard_id uuid,
    system_id uuid,
    owner_ref uuid generated always as (coalesce(standard_id, system_id)) stored,
    name text not null,
    display_name text not null default '',
    position integer not null,
    constraint choice_alternate_name_key unique (choice_id, name),
    constraint choice_alternate_position_key unique (choice_id, position) deferrable initially immediate,
    constraint choice_alternate_owner_fk foreign key (choice_id, owner_kind, owner_ref)
        references role_choice(id, owner_kind, owner_ref),
    -- The FK target for system_role_alternate_fk below, the same reason
    -- role_choice_owner_key exists: id alone is unique, but the composite
    -- FK still needs an explicit constraint naming exactly these columns.
    constraint choice_alternate_owner_key unique (id, owner_kind, owner_ref)
);

-- alternate_id is ON DELETE RESTRICT, never SET NULL: a role in no
-- alternate is unconditional (#626, internal/health.SystemVerdictWith), so
-- SET NULL would silently PROMOTE every role of a deleted alternate from
-- conditional to mandatory the instant the alternate disappears, taking
-- every conforming system to its worst declared impact with no audit row
-- explaining why. This mirrors the asymmetry role_typed_slots.sql already
-- documents (20260807120000_role_typed_slots.sql:5-22): CASCADE on role_id
-- because withdrawing a role withdraws what it requires, RESTRICT on a
-- registry FK because a catalog delete must not silently empty a
-- declaration. The Storage Gateway's DeleteChoice/DeleteAlternate refuse
-- first, naming the roles still attached; this constraint is the backstop
-- for whatever reaches the database directly. alternate_id itself stays
-- nullable and is the only column this FK leaves nullable: NULL is
-- "unconditional", every other referencing column (owner_kind, owner_ref)
-- is always populated, which is what keeps MATCH SIMPLE's null-skips-the-
-- constraint rule from swallowing the whole check (see the header comment).
alter table system_role add column if not exists alternate_id uuid;
alter table system_role add column if not exists owner_ref uuid generated always as (coalesce(standard_id, system_id)) stored;

do $$ begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'system_role'::regclass and conname = 'system_role_alternate_fk'
  ) then
    alter table system_role add constraint system_role_alternate_fk
      foreign key (alternate_id, owner_kind, owner_ref)
      references choice_alternate(id, owner_kind, owner_ref) on delete restrict;
  end if;
end $$;

-- system_role's missing owner arc (#626): roleOwnerExpr resolves the owner
-- with a scalar subquery keyed on the caller's name, so an unknown standard
-- or system silently resolves to NULL; both owner columns are nullable and
-- nothing has ever refused the result, so a typo'd owner reference creates
-- an ownerless role, and a SECOND typo then silently UPDATEs the first
-- orphan under system_role_name_key's NULLS NOT DISTINCT. Closed here,
-- riding along with an already-open migration, because role_choice would
-- otherwise inherit the identical hole verbatim. Raise first: an upgrade
-- that already carries an ownerless row has nothing this constraint alone
-- can repair.
do $$
declare bad bigint;
begin
  select count(*) into bad from system_role
   where (owner_kind = 'standard' and (standard_id is null or system_id is not null))
      or (owner_kind = 'system'   and (system_id is null or standard_id is not null));
  if bad > 0 then
    raise exception 'system_role owner arc (#626): % ownerless or double-owned role(s); repair before upgrading', bad;
  end if;
end $$;

do $$ begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'system_role'::regclass and conname = 'system_role_owner_arc_check'
  ) then
    alter table system_role add constraint system_role_owner_arc_check check (
      (owner_kind = 'standard' and standard_id is not null and system_id is null) or
      (owner_kind = 'system'   and system_id is not null and standard_id is null));
  end if;
end $$;

-- migrate:down

alter table system_role drop constraint if exists system_role_owner_arc_check;
alter table system_role drop constraint if exists system_role_alternate_fk;
alter table system_role drop column if exists owner_ref;
alter table system_role drop column if exists alternate_id;
drop table if exists choice_alternate;
drop table if exists role_choice;
