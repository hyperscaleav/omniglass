-- migrate:up

-- #626 fix-round repair. choice_alternate_position_key was created
-- non-deferrable in 20260807150000_role_choices_and_alternates.sql, then
-- edited in place, within the same development session, to add DEFERRABLE
-- INITIALLY IMMEDIATE, because SeedRoleChoice needs to reassign every
-- alternate's position in one pass (see the comment on the constraint
-- itself and on SeedRoleChoice). Editing that file in place was safe for a
-- database that had not yet applied version 20260807150000, but dbmate
-- keys on the version, never the contents, and 20260807150000's own
-- `create table if not exists` makes even a forced re-run a no-op: a
-- database that already applied that version before this repair landed
-- keeps the constraint non-deferrable forever unless something else fixes
-- it. SeedRoleChoice unconditionally issues `set constraints
-- choice_alternate_position_key deferred` on every boot, and Postgres
-- refuses to defer a NAMED constraint that is not itself DEFERRABLE (unlike
-- the ALL form, which just skips it), so such a database cannot boot at
-- all, on every start, until this migration runs.
--
-- This is therefore its own migration rather than a further in-place edit
-- of 20260807150000: the never-edit-an-applied-migration rule exists
-- precisely to keep a chain converging regardless of which shape a given
-- database already has, and a database that applied the old shape is a
-- real, live possibility (any dev box that checked out this branch before
-- this repair). The guard makes this a no-op on a fresh chain, where
-- 20260807150000 already creates the constraint deferrable.
do $$ begin
  if exists (
    select 1 from pg_constraint
    where conname = 'choice_alternate_position_key'
      and conrelid = 'choice_alternate'::regclass
      and not condeferrable
  ) then
    alter table choice_alternate drop constraint choice_alternate_position_key;
    alter table choice_alternate add constraint choice_alternate_position_key
      unique (choice_id, position) deferrable initially immediate;
  end if;
end $$;

-- migrate:down

do $$ begin
  if exists (
    select 1 from pg_constraint
    where conname = 'choice_alternate_position_key'
      and conrelid = 'choice_alternate'::regclass
      and condeferrable
  ) then
    alter table choice_alternate drop constraint choice_alternate_position_key;
    alter table choice_alternate add constraint choice_alternate_position_key
      unique (choice_id, position);
  end if;
end $$;
