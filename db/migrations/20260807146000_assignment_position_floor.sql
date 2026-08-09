-- migrate:up

-- The floor: after the backfill, every assignment has a position, so it
-- becomes required and gets its two invariants.
--
-- The uniqueness constraint is a DEFERRABLE CONSTRAINT, not a unique index:
-- CREATE UNIQUE INDEX cannot be declared DEFERRABLE, and a plain unique
-- index is checked per updated tuple rather than at end of statement, so a
-- single-statement swap (SwapPositions) would raise 23505 the moment the
-- first row lands on the other row's position. SwapPositions defers this
-- constraint for the rest of its transaction before its two-row update. No
-- ON CONFLICT may ever name this constraint as its arbiter: Postgres refuses
-- a deferrable constraint as an ON CONFLICT arbiter. AssignRole's own
-- arbiter is the unrelated, non-deferrable (system_id, role_id,
-- component_id) key (init.sql:1319-1320), and is unaffected.
alter table system_role_assignment alter column position set not null;

do $$ begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'system_role_assignment'::regclass and conname = 'system_role_assignment_position_check'
  ) then
    alter table system_role_assignment add constraint system_role_assignment_position_check
      check (position >= 1);
  end if;
end $$;

do $$ begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'system_role_assignment'::regclass and conname = 'system_role_assignment_position_key'
  ) then
    alter table system_role_assignment add constraint system_role_assignment_position_key
      unique (system_id, role_id, position) deferrable initially immediate;
  end if;
end $$;

-- migrate:down

alter table system_role_assignment drop constraint if exists system_role_assignment_position_key;
alter table system_role_assignment drop constraint if exists system_role_assignment_position_check;
alter table system_role_assignment alter column position drop not null;
