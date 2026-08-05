-- migrate:up

-- #590: a command records its status, and an intended value names its command.
--
-- Three moves. command gains a recorded status (issued until a terminal
-- settled/failed/timed-out, with settled_at stamping the terminal moment), so
-- a stuck command is distinguishable from one nobody has checked. command_type
-- gains the two-armed target arc (a command may set a property or a metric,
-- never both), because a metric is commandable: "set volume to 50" opens an
-- intended numeric. And both sample series gain command_id, the lineage of an
-- intended value: a value is pointed at the command that caused it rather than
-- at the event derived from that command, so the intended arm of the lineage
-- CHECK moves from requiring event_id to requiring command_id (the caused
-- event stays a stamped, now-optional column).

alter table command add column if not exists status text not null default 'issued';
alter table command add column if not exists settled_at timestamptz;
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'command_status_check') then
    alter table command add constraint command_status_check
      check (status = any (array['issued'::text, 'settled'::text, 'failed'::text, 'timed-out'::text]));
  end if;
  -- settled_at is the terminal moment: absent exactly while the command is
  -- still issued, so a terminal status can never be recorded without it.
  if not exists (select 1 from pg_constraint where conname = 'command_settlement_recorded_check') then
    alter table command add constraint command_settlement_recorded_check
      check ((status = 'issued'::text) = (settled_at is null));
  end if;
end $$;

alter table command_type add column if not exists target_metric_type_id uuid;
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'command_type_target_metric_type_id_fkey') then
    alter table command_type add constraint command_type_target_metric_type_id_fkey
      foreign key (target_metric_type_id) references metric_type(id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_type_target_arc_check') then
    alter table command_type add constraint command_type_target_arc_check
      check (target_property_type_id is null or target_metric_type_id is null);
  end if;
end $$;

alter table property add column if not exists command_id bigint;
alter table metric add column if not exists command_id bigint;
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'property_command_id_fkey') then
    alter table property add constraint property_command_id_fkey
      foreign key (command_id) references command(id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'metric_command_id_fkey') then
    alter table metric add constraint metric_command_id_fkey
      foreign key (command_id) references command(id);
  end if;
end $$;

-- Refuse rather than guess on any intended row whose command cannot be
-- recovered: the only recoverable lineage is the reverse join through the
-- caused event (command.caused_event_id), and a row without one would be left
-- violating the moved CHECK below.
do $$
declare
  offender text;
begin
  select 'property row ' || p.id::text into offender
  from property p
  where p.provenance = 'intended' and p.command_id is null
    and not exists (select 1 from command c where c.caused_event_id = p.event_id)
  limit 1;
  if offender is not null then
    raise exception '% is an intended value whose command cannot be recovered from its event', offender;
  end if;
  select 'metric row ' || m.id::text into offender
  from metric m
  where m.provenance = 'intended' and m.command_id is null
    and not exists (select 1 from command c where c.caused_event_id = m.event_id)
  limit 1;
  if offender is not null then
    raise exception '% is an intended value whose command cannot be recovered from its event', offender;
  end if;
end $$;

-- The repoint: every existing intended row names its event's command. The
-- newest command claiming the event wins the (practically impossible) tie, so
-- the backfill is deterministic. Idempotent: the null guard makes a re-run a
-- no-op.
update property p
set command_id = (select c.id from command c where c.caused_event_id = p.event_id order by c.ts desc, c.id desc limit 1)
where p.provenance = 'intended' and p.command_id is null;
update metric m
set command_id = (select c.id from command c where c.caused_event_id = m.event_id order by c.ts desc, c.id desc limit 1)
where m.provenance = 'intended' and m.command_id is null;

-- The lineage CHECK moves onto command_id for the intended arm; every other
-- provenance refuses command lineage the same way it refuses the arms it does
-- not own. event_id stays stamped on new intended rows but is optional now.
alter table property drop constraint if exists property_lineage_check;
alter table property add constraint property_lineage_check
    check ((((provenance = 'observed'::text) and (event_id is null) and (command_id is null))
        or ((provenance = 'calculated'::text) and (source_rule is not null) and (event_id is null) and (command_id is null))
        or ((provenance = 'intended'::text) and (command_id is not null) and (source_rule is null))
        or ((provenance = 'declared'::text) and (source_rule is null) and (event_id is null) and (command_id is null))));
alter table metric drop constraint if exists metric_lineage_check;
alter table metric add constraint metric_lineage_check
    check ((((provenance = 'observed'::text) and (event_id is null) and (command_id is null))
        or ((provenance = 'calculated'::text) and (source_rule is not null) and (event_id is null) and (command_id is null))
        or ((provenance = 'intended'::text) and (command_id is not null) and (source_rule is null))));

-- migrate:down

-- The old shape requires event lineage on every intended row; restore it from
-- the command's caused event before the CHECK swaps back, refusing loudly on a
-- row that has neither (the same refuse-not-guess as the up).
update property p
set event_id = (select c.caused_event_id from command c where c.id = p.command_id)
where p.provenance = 'intended' and p.event_id is null and p.command_id is not null;
update metric m
set event_id = (select c.caused_event_id from command c where c.id = m.command_id)
where m.provenance = 'intended' and m.event_id is null and m.command_id is not null;
do $$
declare
  offender text;
begin
  select 'property row ' || p.id::text into offender
  from property p where p.provenance = 'intended' and p.event_id is null limit 1;
  if offender is not null then
    raise exception '% is an intended value with no caused event to restore', offender;
  end if;
  select 'metric row ' || m.id::text into offender
  from metric m where m.provenance = 'intended' and m.event_id is null limit 1;
  if offender is not null then
    raise exception '% is an intended value with no caused event to restore', offender;
  end if;
  -- A metric-targeted command_type has no representation in the old schema.
  select 'command_type ' || ct.name into offender
  from command_type ct where ct.target_metric_type_id is not null limit 1;
  if offender is not null then
    raise exception '% targets a metric, which the schema below this migration cannot represent', offender;
  end if;
end $$;

alter table property drop constraint if exists property_lineage_check;
alter table property add constraint property_lineage_check
    check ((((provenance = 'observed'::text) and (event_id is null))
        or ((provenance = 'calculated'::text) and (source_rule is not null) and (event_id is null))
        or ((provenance = 'intended'::text) and (event_id is not null) and (source_rule is null))
        or ((provenance = 'declared'::text) and (source_rule is null) and (event_id is null))));
alter table metric drop constraint if exists metric_lineage_check;
alter table metric add constraint metric_lineage_check
    check ((((provenance = 'observed'::text) and (event_id is null))
        or ((provenance = 'calculated'::text) and (source_rule is not null) and (event_id is null))
        or ((provenance = 'intended'::text) and (event_id is not null) and (source_rule is null))));

alter table property drop column if exists command_id;
alter table metric drop column if exists command_id;

alter table command_type drop constraint if exists command_type_target_arc_check;
alter table command_type drop column if exists target_metric_type_id;

alter table command drop constraint if exists command_settlement_recorded_check;
alter table command drop constraint if exists command_status_check;
alter table command drop column if exists status;
alter table command drop column if exists settled_at;
