-- migrate:up

-- The raw-log ingest lane (ADR-0066): log_line is arrival, not an event. Untyped
-- text off a firehose, owned through the same exclusive arc as the rest of the
-- estate. severity and facility are promoted to indexed columns because retention
-- and routing key on them; everything else classifiable lives in freeform labels
-- and attributes. A rule later derives typed events from these lines; most never
-- become events at all.
create table if not exists log_line (
    id bigint generated always as identity,
    ts timestamptz default now() not null,
    owner_kind text not null,
    instance text default ''::text not null,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    source text default ''::text not null,
    severity text,
    facility text,
    message text not null,
    attributes jsonb,
    labels jsonb,
    correlation_id text
);
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'log_line_pkey') then
    alter table log_line add constraint log_line_pkey primary key (id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'log_line_component_id_fkey') then
    alter table log_line add constraint log_line_component_id_fkey foreign key (component_id) references component(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'log_line_system_id_fkey') then
    alter table log_line add constraint log_line_system_id_fkey foreign key (system_id) references system(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'log_line_location_id_fkey') then
    alter table log_line add constraint log_line_location_id_fkey foreign key (location_id) references location(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'log_line_node_id_fkey') then
    alter table log_line add constraint log_line_node_id_fkey foreign key (node_id) references node(principal_id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'log_line_owner_kind_check') then
    alter table log_line add constraint log_line_owner_kind_check
      check (owner_kind = any (array['component'::text, 'system'::text, 'location'::text, 'node'::text]));
  end if;
  if not exists (select 1 from pg_constraint where conname = 'log_line_owner_arc_check') then
    alter table log_line add constraint log_line_owner_arc_check check (
      ((owner_kind = 'component'::text) and (component_id is not null) and (system_id is null) and (location_id is null) and (node_id is null)) or
      ((owner_kind = 'system'::text) and (system_id is not null) and (component_id is null) and (location_id is null) and (node_id is null)) or
      ((owner_kind = 'location'::text) and (location_id is not null) and (component_id is null) and (system_id is null) and (node_id is null)) or
      ((owner_kind = 'node'::text) and (node_id is not null) and (component_id is null) and (system_id is null) and (location_id is null))
    );
  end if;
end $$;

-- Reads are newest-first per owner; severity and facility are the retention and
-- routing axes.
create index if not exists log_line_component_ts_idx on log_line (component_id, ts desc) where component_id is not null;
create index if not exists log_line_system_ts_idx on log_line (system_id, ts desc) where system_id is not null;
create index if not exists log_line_location_ts_idx on log_line (location_id, ts desc) where location_id is not null;
create index if not exists log_line_node_ts_idx on log_line (node_id, ts desc) where node_id is not null;
create index if not exists log_line_severity_idx on log_line (severity) where severity is not null;
create index if not exists log_line_facility_idx on log_line (facility) where facility is not null;

-- migrate:down

drop table if exists log_line;
