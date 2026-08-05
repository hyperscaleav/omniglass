-- migrate:up

-- A log line belongs to a component (#589). Systems, locations, and nodes do
-- not emit logs, so log_line's four-armed owner arc narrows to component-only:
-- the three foreign arms drop, and with a single owner there is nothing left
-- for owner_kind or the arc CHECK to discriminate.
--
-- The node arm was not dead, though: a node ships its own self-logs over the
-- raw ingest lane (ADR-0066) and they landed node-owned here. Narrowing alone
-- would orphan that shipped behavior, so the self-logs split into node_log
-- first: an origin-true table (every row is node-owned) with the log_line
-- payload shape minus the arc, keyed to node(principal_id) with the same
-- cascade the arm had. Logs are never alerted on directly (events are derived
-- from them), so splitting the store by origin costs nothing downstream.

create table if not exists node_log (
    id bigint generated always as identity,
    ts timestamptz default now() not null,
    node_id uuid not null,
    source text default ''::text not null,
    severity text,
    facility text,
    message text not null,
    attributes jsonb,
    labels jsonb,
    correlation_id text
);
do $$ begin
  if not exists (select 1 from pg_constraint where conrelid = 'node_log'::regclass and conname = 'node_log_pkey') then
    alter table node_log add constraint node_log_pkey primary key (id);
  end if;
  if not exists (select 1 from pg_constraint where conrelid = 'node_log'::regclass and conname = 'node_log_node_id_fkey') then
    alter table node_log add constraint node_log_node_id_fkey
      foreign key (node_id) references node(principal_id) on delete cascade;
  end if;
end $$;
-- The list read is newest-first per node; one composite index serves it.
create index if not exists node_log_node_ts_idx on node_log (node_id, ts desc);

-- Move the node arm's rows into their new home, then refuse rather than drop
-- anything else the narrowing cannot carry: a system- or location-owned line
-- has never been written (no writer exists), so pre-release this raises on
-- nothing, and on a hypothetical estate it names what it found instead of
-- silently deleting operator data. Guarded on owner_kind still existing so a
-- re-run against the narrowed table is a no-op rather than an error.
--
-- New identities for the moved rows are fine: the only reference to a log
-- row's id is event.source_log_line_id, which has no writer yet, so no
-- lineage exists to preserve.
do $$
declare
  bad_count bigint;
  bad_kinds text;
begin
  if exists (select 1 from information_schema.columns
             where table_name = 'log_line' and column_name = 'owner_kind') then
    insert into node_log (ts, node_id, source, severity, facility, message, attributes, labels, correlation_id)
    select ts, node_id, source, severity, facility, message, attributes, labels, correlation_id
    from log_line where owner_kind = 'node';
    delete from log_line where owner_kind = 'node';

    select count(*), string_agg(distinct owner_kind, ', ')
      into bad_count, bad_kinds
      from log_line where owner_kind <> 'component';
    if bad_count > 0 then
      raise exception 'log_line narrowing (#589): % rows remain with owner_kind in (%); a log line belongs to a component and these cannot be carried',
        bad_count, bad_kinds;
    end if;
  end if;
end $$;

-- The narrowing. Dropping each arm column also drops its FK and partial ts
-- index; the constraints are dropped explicitly first anyway so the intent
-- reads in full. The surviving owner becomes mandatory.
alter table log_line drop constraint if exists log_line_owner_arc_check;
alter table log_line drop constraint if exists log_line_owner_kind_check;
alter table log_line drop constraint if exists log_line_system_id_fkey;
alter table log_line drop constraint if exists log_line_location_id_fkey;
alter table log_line drop constraint if exists log_line_node_id_fkey;
alter table log_line drop column if exists owner_kind;
alter table log_line drop column if exists system_id;
alter table log_line drop column if exists location_id;
alter table log_line drop column if exists node_id;
alter table log_line alter column component_id set not null;

-- migrate:down

-- Restore the four-armed shape exactly as the chain's up built it (the
-- 20260728120000 columns, FKs, CHECKs, and partial indexes), move the
-- self-logs back onto the node arm, and drop node_log. owner_kind gets a
-- 'component' default so the restore can stamp the surviving rows before the
-- NOT NULL lands.

alter table log_line alter column component_id drop not null;
alter table log_line add column if not exists owner_kind text;
alter table log_line add column if not exists system_id uuid;
alter table log_line add column if not exists location_id uuid;
alter table log_line add column if not exists node_id uuid;
update log_line set owner_kind = 'component' where owner_kind is null;
alter table log_line alter column owner_kind set default 'component'::text;
alter table log_line alter column owner_kind set not null;
do $$ begin
  if not exists (select 1 from pg_constraint where conrelid = 'log_line'::regclass and conname = 'log_line_system_id_fkey') then
    alter table log_line add constraint log_line_system_id_fkey
      foreign key (system_id) references system(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conrelid = 'log_line'::regclass and conname = 'log_line_location_id_fkey') then
    alter table log_line add constraint log_line_location_id_fkey
      foreign key (location_id) references location(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conrelid = 'log_line'::regclass and conname = 'log_line_node_id_fkey') then
    alter table log_line add constraint log_line_node_id_fkey
      foreign key (node_id) references node(principal_id) on delete cascade;
  end if;
end $$;

-- The self-logs ride back onto the node arm before the arc CHECK lands, so
-- the CHECK never sees a half-restored row. Guarded on node_log existing so a
-- re-run after the drop below is a no-op.
do $$ begin
  if to_regclass('node_log') is not null then
    insert into log_line (ts, owner_kind, node_id, source, severity, facility, message, attributes, labels, correlation_id)
    select ts, 'node', node_id, source, severity, facility, message, attributes, labels, correlation_id
    from node_log;
  end if;
end $$;
drop table if exists node_log;

do $$ begin
  if not exists (select 1 from pg_constraint where conrelid = 'log_line'::regclass and conname = 'log_line_owner_kind_check') then
    alter table log_line add constraint log_line_owner_kind_check
      check (owner_kind = any (array['component'::text, 'system'::text, 'location'::text, 'node'::text]));
  end if;
  if not exists (select 1 from pg_constraint where conrelid = 'log_line'::regclass and conname = 'log_line_owner_arc_check') then
    alter table log_line add constraint log_line_owner_arc_check check (
      ((owner_kind = 'component'::text) and (component_id is not null) and (system_id is null) and (location_id is null) and (node_id is null)) or
      ((owner_kind = 'system'::text) and (system_id is not null) and (component_id is null) and (location_id is null) and (node_id is null)) or
      ((owner_kind = 'location'::text) and (location_id is not null) and (component_id is null) and (system_id is null) and (node_id is null)) or
      ((owner_kind = 'node'::text) and (node_id is not null) and (component_id is null) and (system_id is null) and (location_id is null))
    );
  end if;
end $$;
create index if not exists log_line_system_ts_idx on log_line (system_id, ts desc) where system_id is not null;
create index if not exists log_line_location_ts_idx on log_line (location_id, ts desc) where location_id is not null;
create index if not exists log_line_node_ts_idx on log_line (node_id, ts desc) where node_id is not null;
