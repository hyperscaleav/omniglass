-- migrate:up

-- ADR-0063 #396: the command pillar, the "do" half of the telemetry model. command_type
-- is the driver-owned catalog of what a component can be told (params + a settle_window,
-- the driver's fact about the device's actuation timing), the third registry alongside
-- property_type (know) and event_type (happen). command is the invocation log over the
-- owner arc. Settlement is computed, never stored.

create table if not exists command_type (
    id uuid default uuidv7() not null,
    name text not null,
    display_name text,
    description text default ''::text not null,
    params_schema jsonb,
    settle_window_seconds integer default 0 not null,
    target_property_type_id uuid,
    official boolean default false not null,
    registered_at timestamptz default now() not null
);
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'command_type_pkey') then
    alter table command_type add constraint command_type_pkey primary key (id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_type_name_key') then
    alter table command_type add constraint command_type_name_key unique (name);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_type_target_property_type_id_fkey') then
    alter table command_type add constraint command_type_target_property_type_id_fkey
      foreign key (target_property_type_id) references property_type(id);
  end if;
end $$;

create table if not exists command (
    id bigint generated always as identity,
    ts timestamptz default now() not null,
    owner_kind text not null,
    instance text default ''::text not null,
    params jsonb,
    actor uuid,
    component_id uuid,
    system_id uuid,
    location_id uuid,
    node_id uuid,
    command_type_id uuid not null,
    caused_event_id bigint
);
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'command_pkey') then
    alter table command add constraint command_pkey primary key (id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_command_type_id_fkey') then
    alter table command add constraint command_command_type_id_fkey foreign key (command_type_id) references command_type(id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_caused_event_id_fkey') then
    alter table command add constraint command_caused_event_id_fkey foreign key (caused_event_id) references event(id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_component_id_fkey') then
    alter table command add constraint command_component_id_fkey foreign key (component_id) references component(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_system_id_fkey') then
    alter table command add constraint command_system_id_fkey foreign key (system_id) references system(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_location_id_fkey') then
    alter table command add constraint command_location_id_fkey foreign key (location_id) references location(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_node_id_fkey') then
    alter table command add constraint command_node_id_fkey foreign key (node_id) references node(principal_id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_owner_kind_check') then
    alter table command add constraint command_owner_kind_check
      check (owner_kind = any (array['component'::text, 'system'::text, 'location'::text, 'node'::text]));
  end if;
  if not exists (select 1 from pg_constraint where conname = 'command_owner_arc_check') then
    alter table command add constraint command_owner_arc_check check (
      ((owner_kind = 'component'::text) and (component_id is not null) and (system_id is null) and (location_id is null) and (node_id is null)) or
      ((owner_kind = 'system'::text) and (system_id is not null) and (component_id is null) and (location_id is null) and (node_id is null)) or
      ((owner_kind = 'location'::text) and (location_id is not null) and (component_id is null) and (system_id is null) and (node_id is null)) or
      ((owner_kind = 'node'::text) and (node_id is not null) and (component_id is null) and (system_id is null) and (location_id is null))
    );
  end if;
end $$;

-- migrate:down

drop table if exists command;
drop table if exists command_type;
