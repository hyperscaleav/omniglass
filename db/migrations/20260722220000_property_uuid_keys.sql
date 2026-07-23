-- migrate:up

-- property takes a uuid primary key; its name becomes a unique, renameable
-- handle. Slice 3 of the registry epic, and the largest: property is referenced
-- four ways by contract/value tables AND three ways by telemetry, where the
-- `key` column was a loose property name with no foreign key.
--
-- The telemetry keys become real foreign keys (key -> property_id), so a rename
-- follows an observation series too, not only a contract. Every telemetry key is
-- already a registered property: reject-not-project drops an unregistered name at
-- collection, so the constraint only makes the invariant the database's as well.

create or replace function pg_temp.drop_pk(tbl text) returns void language plpgsql as $$
declare c text;
begin
  select conname into c from pg_constraint where contype = 'p' and conrelid = tbl::regclass;
  if c is not null then execute format('alter table %I drop constraint %I', tbl, c); end if;
end $$;

create or replace function pg_temp.drop_refs(tbl text) returns void language plpgsql as $$
declare r record;
begin
  for r in select conrelid::regclass::text as t, conname from pg_constraint
            where contype = 'f' and confrelid = tbl::regclass loop
    execute format('alter table %s drop constraint %I', r.t, r.conname);
  end loop;
end $$;

select pg_temp.drop_refs('property');

alter table property rename column name to handle;
alter table property add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('property');
alter table property add constraint property_pkey primary key (uuid_id);
alter table property add constraint property_handle_key unique (handle);
alter table property rename column uuid_id to id;
alter table property rename column handle to name;

-- contract/value tables: property_name text -> property_id uuid --------------
alter table product_property drop constraint if exists product_property_product_id_property_name_key;
alter table product_property add column if not exists property_uuid uuid;
update product_property x set property_uuid = p.id from property p where p.name = x.property_name;
alter table product_property drop column property_name;
alter table product_property rename column property_uuid to property_id;
alter table product_property add constraint product_property_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;
alter table product_property add constraint product_property_product_id_property_id_key
    unique (product_id, property_id);
create index if not exists product_property_property_idx on product_property (property_id);

alter table standard_property drop constraint if exists standard_property_standard_id_property_name_key;
alter table standard_property add column if not exists property_uuid uuid;
update standard_property x set property_uuid = p.id from property p where p.name = x.property_name;
alter table standard_property drop column property_name;
alter table standard_property rename column property_uuid to property_id;
alter table standard_property add constraint standard_property_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;
alter table standard_property add constraint standard_property_standard_id_property_id_key
    unique (standard_id, property_id);
create index if not exists standard_property_property_idx on standard_property (property_id);

alter table location_type_property drop constraint if exists location_type_property_location_type_id_property_name_key;
alter table location_type_property add column if not exists property_uuid uuid;
update location_type_property x set property_uuid = p.id from property p where p.name = x.property_name;
alter table location_type_property drop column property_name;
alter table location_type_property rename column property_uuid to property_id;
alter table location_type_property add constraint location_type_property_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;
alter table location_type_property add constraint location_type_property_location_type_id_property_id_key
    unique (location_type_id, property_id);
create index if not exists location_type_property_property_idx on location_type_property (property_id);

alter table property_value drop constraint if exists property_value_series_key;
alter table property_value add column if not exists property_uuid uuid;
update property_value x set property_uuid = p.id from property p where p.name = x.property_name;
alter table property_value drop column property_name;
alter table property_value rename column property_uuid to property_id;
alter table property_value alter column property_id set not null;
alter table property_value add constraint property_value_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;
alter table property_value add constraint property_value_series_key unique nulls not distinct
    (owner_kind, component_id, system_id, location_id, node_id, property_id, instance, provenance);
create index if not exists property_value_component_idx on property_value (component_id, property_id) where component_id is not null;

-- telemetry: key text -> property_id uuid, a NEW foreign key -------------------
alter table metric_datapoint add column if not exists property_id uuid;
update metric_datapoint d set property_id = p.id from property p where p.name = d.key;
alter table metric_datapoint drop column key;
alter table metric_datapoint alter column property_id set not null;
alter table metric_datapoint add constraint metric_datapoint_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;
create index if not exists metric_datapoint_owner_idx on metric_datapoint (component_id, property_id, instance, ts desc) where component_id is not null;

alter table state_datapoint add column if not exists property_id uuid;
update state_datapoint d set property_id = p.id from property p where p.name = d.key;
alter table state_datapoint drop column key;
alter table state_datapoint alter column property_id set not null;
alter table state_datapoint add constraint state_datapoint_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;

alter table event add column if not exists property_id uuid;
update event d set property_id = p.id from property p where p.name = d.key;
alter table event drop column key;
alter table event alter column property_id set not null;
alter table event add constraint event_property_id_fkey
    foreign key (property_id) references property (id) on delete cascade;
create index if not exists event_owner_idx on event (component_id, property_id, instance, ts desc) where component_id is not null;

-- migrate:down

-- No release exists, so the reversal restores the shape. Every reference points
-- at a property that still exists, so its current name is the right answer.

alter table event drop constraint if exists event_property_id_fkey;
drop index if exists event_owner_idx;
alter table event add column if not exists key text;
update event d set key = p.name from property p where p.id = d.property_id;
alter table event alter column key set not null;
alter table event drop column property_id;
create index if not exists event_owner_idx on event (component_id, key, instance, ts desc) where component_id is not null;

alter table state_datapoint drop constraint if exists state_datapoint_property_id_fkey;
alter table state_datapoint add column if not exists key text;
update state_datapoint d set key = p.name from property p where p.id = d.property_id;
alter table state_datapoint alter column key set not null;
alter table state_datapoint drop column property_id;

alter table metric_datapoint drop constraint if exists metric_datapoint_property_id_fkey;
drop index if exists metric_datapoint_owner_idx;
alter table metric_datapoint add column if not exists key text;
update metric_datapoint d set key = p.name from property p where p.id = d.property_id;
alter table metric_datapoint alter column key set not null;
alter table metric_datapoint drop column property_id;
create index if not exists metric_datapoint_owner_idx on metric_datapoint (component_id, key, instance, ts desc) where component_id is not null;

alter table property_value drop constraint if exists property_value_property_id_fkey;
alter table property_value drop constraint if exists property_value_series_key;
drop index if exists property_value_component_idx;
alter table property_value add column if not exists property_name text;
update property_value x set property_name = p.name from property p where p.id = x.property_id;
alter table property_value alter column property_name set not null;
alter table property_value drop column property_id;
alter table property_value add constraint property_value_series_key unique nulls not distinct
    (owner_kind, component_id, system_id, location_id, node_id, property_name, instance, provenance);
create index if not exists property_value_component_idx on property_value (component_id, property_name) where component_id is not null;

alter table location_type_property drop constraint if exists location_type_property_property_id_fkey;
alter table location_type_property drop constraint if exists location_type_property_location_type_id_property_id_key;
drop index if exists location_type_property_property_idx;
alter table location_type_property add column if not exists property_name text;
update location_type_property x set property_name = p.name from property p where p.id = x.property_id;
alter table location_type_property alter column property_name set not null;
alter table location_type_property drop column property_id;
alter table location_type_property add constraint location_type_property_location_type_id_property_name_key unique (location_type_id, property_name);

alter table standard_property drop constraint if exists standard_property_property_id_fkey;
alter table standard_property drop constraint if exists standard_property_standard_id_property_id_key;
drop index if exists standard_property_property_idx;
alter table standard_property add column if not exists property_name text;
update standard_property x set property_name = p.name from property p where p.id = x.property_id;
alter table standard_property alter column property_name set not null;
alter table standard_property drop column property_id;
alter table standard_property add constraint standard_property_standard_id_property_name_key unique (standard_id, property_name);

alter table product_property drop constraint if exists product_property_property_id_fkey;
alter table product_property drop constraint if exists product_property_product_id_property_id_key;
drop index if exists product_property_property_idx;
alter table product_property add column if not exists property_name text;
update product_property x set property_name = p.name from property p where p.id = x.property_id;
alter table product_property alter column property_name set not null;
alter table product_property drop column property_id;
alter table product_property add constraint product_property_product_id_property_name_key unique (product_id, property_name);

alter table property drop constraint if exists property_pkey;
alter table property drop constraint if exists property_handle_key;
alter table property drop column id;
alter table property add primary key (name);

alter table product_property add constraint product_property_property_name_fkey
    foreign key (property_name) references property (name) on delete cascade;
alter table standard_property add constraint standard_property_property_name_fkey
    foreign key (property_name) references property (name) on delete cascade;
alter table location_type_property add constraint location_type_property_property_name_fkey
    foreign key (property_name) references property (name) on delete cascade;
alter table property_value add constraint property_value_property_name_fkey
    foreign key (property_name) references property (name) on delete cascade;
