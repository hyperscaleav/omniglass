-- migrate:up

-- #626 (task 6): a role declares how many occupants it will accept beyond its
-- quorum floor (capacity, an optional upper bound; null means unbounded) and,
-- for a surface that labels each slot, what to call them (position_labels, by
-- index; resolved against an assignment's position once the next migration
-- adds one). Both are plain columns beside quorum and impact: neither varies
-- per assignment, only per declaration. Named capacity rather than max: max
-- shadows the Go builtin the very file that writes it already calls
-- (role_declarations.go's SeedSystemRole), and reads as a function call
-- everywhere it appears unqualified in SQL.
alter table system_role add column if not exists capacity integer;
alter table system_role add column if not exists position_labels text[] not null default '{}';

do $$ begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'system_role'::regclass and conname = 'system_role_capacity_check'
  ) then
    alter table system_role add constraint system_role_capacity_check
      check (capacity is null or capacity >= quorum);
  end if;
end $$;

-- One role per component per system (#626): nothing has ever enforced this.
-- The only unique key on system_role_assignment is (system_id, role_id,
-- component_id), which permits the same component under a second role_id in
-- the same system, and internal/storage/health.go's staffing memo states the
-- opposite invariant in prose. A bare CREATE UNIQUE INDEX would abort
-- mid-migration on any estate that already has such a row, with a 23505
-- naming no rows, and migrations run exactly once and are never edited after
-- landing, so a stuck deployment would have nothing to act on. Raise first,
-- naming the offending system/component pairs, so an upgrade that cannot
-- proceed at least says why.
do $$
declare
  bad_count bigint;
  bad_rows text;
begin
  select count(*), string_agg(format('%s/%s', s.name, c.name), ', ')
    into bad_count, bad_rows
    from (
      select system_id, component_id
      from system_role_assignment
      group by system_id, component_id
      having count(*) > 1
    ) d
    join system s on s.id = d.system_id
    join component c on c.id = d.component_id;
  if bad_count > 0 then
    raise exception 'one role per component (#626): % component(s) fill more than one role in their system (%); unassign the extra roles before upgrading', bad_count, bad_rows;
  end if;
end $$;

create unique index if not exists system_role_assignment_component_key
  on system_role_assignment (system_id, component_id);

-- migrate:down

drop index if exists system_role_assignment_component_key;
alter table system_role drop constraint if exists system_role_capacity_check;
alter table system_role drop column if exists position_labels;
alter table system_role drop column if exists capacity;
