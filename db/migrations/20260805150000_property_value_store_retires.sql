-- migrate:up

-- The property VALUE STORE retires (#591). A declared value is a series row
-- like any other, distinguished by provenance='declared', and the current
-- value of every provenance is the latest series row per (type, owner arc,
-- instance, provenance), so the store has nothing left to hold: not the
-- declared values, not the intended ones, and not the latest-value cache.
-- Every row folds into the text sample series (`state`), because the slice-B
-- split (#587) already guarantees the store references no numeric-lane type
-- (its guard raised otherwise); asserted again below anyway. Dropping the
-- table frees the bare name `property` for the state rename (#588).
--
-- Trap, carried from the split and still live: `property` here is the VALUE
-- STORE and `property_type` is the catalog, so every reference below names
-- property_type explicitly. After the drop at the end the name `property` is
-- FREE, and a stray reference to it fails loudly instead of binding.

-- The series re-admits declared rows. #460 narrowed state's provenance domain
-- when declared lived only in the value store; the fold puts declared back on
-- the series, restoring the no-lineage arm it had before (a declared value is
-- an operator assertion: no rule, no causing event).
alter table state drop constraint if exists state_provenance_check;
alter table state add constraint state_provenance_check
    check (provenance = any (array['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text]));
alter table state drop constraint if exists state_lineage_check;
alter table state add constraint state_lineage_check
    check ((((provenance = 'observed'::text) and (event_id is null))
        or ((provenance = 'calculated'::text) and (source_rule is not null) and (event_id is null))
        or ((provenance = 'intended'::text) and (event_id is not null) and (source_rule is null))
        or ((provenance = 'declared'::text) and (source_rule is null) and (event_id is null))));

-- Refuse rather than guess on any row the fold cannot carry faithfully.
-- Pre-release these raise on nothing; on a hypothetical estate they name the
-- row instead of folding it wrong (the slice-B pattern).
do $$
declare
  offender text;
begin
  -- A store row of a numeric-lane type. The slice-B guard already blocked
  -- this before moving the type; assert it held.
  select 'value ' || p.id::text || ' of type ' || mt.name into offender
  from property p join metric_type mt on mt.id = p.property_type_id limit 1;
  if offender is not null then
    raise exception 'value store holds %, a metric-lane type; it cannot fold into the text series', offender;
  end if;
  -- A store row whose type the property catalog does not know. The FK makes
  -- this unreachable; asserted so a future constraint change cannot silently
  -- fold an orphan.
  select 'value ' || p.id::text into offender
  from property p left join property_type pt on pt.id = p.property_type_id
  where pt.id is null limit 1;
  if offender is not null then
    raise exception 'value store holds % of a type the catalog does not know', offender;
  end if;
  -- A calculated row would need the source_rule lineage the store never held.
  -- No writer ever produced one; refuse if a hand edit did.
  select 'value ' || p.id::text into offender
  from property p where p.provenance = 'calculated' limit 1;
  if offender is not null then
    raise exception 'value store holds %, provenance calculated, whose rule lineage the store never recorded', offender;
  end if;
  -- An intended row folds with the caused event of the command that opened it
  -- (the series' intended arm requires event lineage); refuse one whose
  -- command cannot be found rather than inventing a cause.
  select 'value ' || p.id::text into offender
  from property p
  where p.provenance = 'intended'
    and not exists (
      select 1 from command c
      join command_type ct on ct.id = c.command_type_id
      where ct.target_property_type_id = p.property_type_id
        and c.owner_kind = p.owner_kind
        and c.component_id is not distinct from p.component_id
        and c.system_id is not distinct from p.system_id
        and c.location_id is not distinct from p.location_id
        and c.node_id is not distinct from p.node_id
        and c.instance = p.instance
        and c.caused_event_id is not null)
  limit 1;
  if offender is not null then
    raise exception 'value store holds %, provenance intended, with no command to recover its event lineage from', offender;
  end if;
end $$;

-- The fold. Every surviving store row becomes a state-series row: the owner
-- arc columns carry over, ts is the value's own time when the store recorded
-- one else the row-write time (coalesce(ts, updated_at)), value is the jsonb
-- scalar as text and value_json keeps the exact jsonb (ADR-0038's structured
-- state value, so bool and json round-trip without loss). An intended row
-- carries the caused event of the newest command that targeted its series.
--
-- Idempotent: state has no unique series constraint (it is append-only), so
-- the fold is insert-where-not-exists on the deterministic key
-- (property_type_id, owner arc, instance, provenance, ts, value): the columns
-- that identify one folded reading. A re-run finds each row already present
-- and inserts nothing.
insert into state (ts, owner_kind, component_id, system_id, location_id, node_id,
                   property_type_id, instance, provenance, value, value_json, event_id)
select coalesce(p.ts, p.updated_at), p.owner_kind, p.component_id, p.system_id, p.location_id, p.node_id,
       p.property_type_id, p.instance, p.provenance,
       coalesce(p.value #>> '{}', ''), p.value,
       case when p.provenance = 'intended' then (
         select c.caused_event_id from command c
         join command_type ct on ct.id = c.command_type_id
         where ct.target_property_type_id = p.property_type_id
           and c.owner_kind = p.owner_kind
           and c.component_id is not distinct from p.component_id
           and c.system_id is not distinct from p.system_id
           and c.location_id is not distinct from p.location_id
           and c.node_id is not distinct from p.node_id
           and c.instance = p.instance
           and c.caused_event_id is not null
         order by c.ts desc, c.id desc limit 1) end
from property p
where not exists (
  select 1 from state s
  where s.property_type_id = p.property_type_id
    and s.owner_kind = p.owner_kind
    and s.component_id is not distinct from p.component_id
    and s.system_id is not distinct from p.system_id
    and s.location_id is not distinct from p.location_id
    and s.node_id is not distinct from p.node_id
    and s.instance = p.instance
    and s.provenance = p.provenance
    and s.ts = coalesce(p.ts, p.updated_at)
    and s.value = coalesce(p.value #>> '{}', ''));

-- With every role folded, the store goes, and the name `property` is free
-- for #588. Anything still referencing the table now fails loudly.
drop table if exists property;

-- migrate:down

-- No down. The fold is a representation change; reversing it would have to
-- guess which series rows had been store rows and rebuild a cache the model
-- no longer has.
select 1;
