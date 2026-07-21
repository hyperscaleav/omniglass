-- migrate:up

-- One-time data backfill: derive membership from the two places it was implied
-- before the table existed. Not a seed and not schema, so it lives in its own
-- migration and runs exactly once.
--
-- Source 1: every component staffing a role is by definition a member of the
-- system it staffs. This is the many-valued half, and it is why the shared device
-- comes through with all of its memberships intact.
insert into system_member (system_id, component_id)
select distinct ra.system_id, ra.component_id
from role_assignment ra
on conflict (system_id, component_id) do nothing;

-- Source 2: the old write-once pointer. It carries components that belonged to a
-- system without filling any declared role (the power conditioner, the rack shelf),
-- which the role table alone would silently drop.
insert into system_member (system_id, component_id)
select distinct s.name, c.name
from component c
join system s on s.id = c.system_id
where c.system_id is not null
on conflict (system_id, component_id) do nothing;

-- The primary is the old pointer's system where there was one, since that is
-- exactly the question the pointer used to answer: which system chain feeds this
-- component's config.
update system_member m
set is_primary = true, updated_at = now()
from component c
join system s on s.id = c.system_id
where m.component_id = c.name
  and m.system_id = s.name
  and c.system_id is not null;

-- Where there was no pointer but the component landed in exactly one system, that
-- membership is unambiguously the default. A component left with several and no
-- pointer keeps none: there is no honest way to guess which one an operator meant,
-- and the resolution paths that matter take a system explicitly anyway.
update system_member m
set is_primary = true, updated_at = now()
where not m.is_primary
  and not exists (
      select 1 from system_member other
      where other.component_id = m.component_id and other.is_primary)
  and (select count(*) from system_member cnt where cnt.component_id = m.component_id) = 1;

-- migrate:down

-- The table is dropped by the schema migration's down; there is nothing to undo
-- here that outlives it, and the rows this created cannot be told apart from rows
-- an operator added afterwards.
select 1;
