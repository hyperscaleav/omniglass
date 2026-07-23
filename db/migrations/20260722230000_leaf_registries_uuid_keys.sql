-- migrate:up

-- The last four registries take uuid primary keys: location_type, interface_type,
-- secret_type, driver. Slice 4 of the registry epic, and after it no foreign key
-- references a mutable string anywhere in the schema.
--
-- interface_type already keyed by `name`, so it only gains the uuid. The other
-- three key by a slug called `id`, so that column is renamed to `name` and the
-- uuid becomes the key: the same two-shape split the registries carried before
-- this epic began.

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

select pg_temp.drop_refs('location_type');
select pg_temp.drop_refs('interface_type');
select pg_temp.drop_refs('secret_type');
select pg_temp.drop_refs('driver');

-- location_type: id -> name, uuid key -----------------------------------------
alter table location_type rename column id to name;
alter table location_type add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('location_type');
alter table location_type add constraint location_type_pkey primary key (uuid_id);
alter table location_type add constraint location_type_name_key unique (name);
alter table location_type rename column uuid_id to id;

-- secret_type: id -> name, uuid key -------------------------------------------
alter table secret_type rename column id to name;
alter table secret_type add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('secret_type');
alter table secret_type add constraint secret_type_pkey primary key (uuid_id);
alter table secret_type add constraint secret_type_name_key unique (name);
alter table secret_type rename column uuid_id to id;

-- driver: id -> name, uuid key ------------------------------------------------
alter table driver rename column id to name;
alter table driver add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('driver');
alter table driver add constraint driver_pkey primary key (uuid_id);
alter table driver add constraint driver_name_key unique (name);
alter table driver rename column uuid_id to id;

-- interface_type: already keyed by name, just add the uuid --------------------
alter table interface_type add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('interface_type');
alter table interface_type add constraint interface_type_pkey primary key (uuid_id);
alter table interface_type add constraint interface_type_name_key unique (name);
alter table interface_type rename column uuid_id to id;

-- repoint every inbound reference ---------------------------------------------
alter table location add column if not exists lt_uuid uuid;
update location x set lt_uuid = t.id from location_type t where t.name = x.location_type;
alter table location drop column location_type;
alter table location rename column lt_uuid to location_type;
alter table location alter column location_type set not null;
alter table location add constraint location_location_type_fkey
    foreign key (location_type) references location_type (id);

alter table location_type_property add column if not exists lt_uuid uuid;
update location_type_property x set lt_uuid = t.id from location_type t where t.name = x.location_type_id;
alter table location_type_property drop column location_type_id;
alter table location_type_property rename column lt_uuid to location_type_id;
alter table location_type_property alter column location_type_id set not null;
alter table location_type_property add constraint location_type_property_location_type_id_fkey
    foreign key (location_type_id) references location_type (id) on delete cascade;
alter table location_type_property add constraint location_type_property_location_type_id_property_id_key
    unique (location_type_id, property_id);

alter table secret add column if not exists st_uuid uuid;
update secret x set st_uuid = t.id from secret_type t where t.name = x.secret_type;
alter table secret drop column secret_type;
alter table secret rename column st_uuid to secret_type;
alter table secret alter column secret_type set not null;
alter table secret add constraint secret_secret_type_fkey
    foreign key (secret_type) references secret_type (id);

alter table product add column if not exists driver_uuid uuid;
update product x set driver_uuid = d.id from driver d where d.name = x.driver_id;
alter table product drop column driver_id;
alter table product rename column driver_uuid to driver_id;
alter table product add constraint product_driver_id_fkey
    foreign key (driver_id) references driver (id) on delete set null;
create index if not exists product_driver_idx on product (driver_id);

alter table interface add column if not exists it_uuid uuid;
update interface x set it_uuid = t.id from interface_type t where t.name = x.type;
alter table interface drop column type;
alter table interface rename column it_uuid to type;
alter table interface alter column type set not null;
alter table interface add constraint interface_type_fkey
    foreign key (type) references interface_type (id);

create index if not exists location_type_name_idx on location_type (name);
create index if not exists secret_type_name_idx on secret_type (name);
create index if not exists driver_name_idx on driver (name);
create index if not exists interface_type_name_idx on interface_type (name);

-- migrate:down

alter table interface drop constraint if exists interface_type_fkey;
alter table interface add column if not exists type_name text;
update interface x set type_name = t.name from interface_type t where t.id = x.type;
alter table interface drop column type;
alter table interface rename column type_name to type;
alter table interface alter column type set not null;

alter table product drop constraint if exists product_driver_id_fkey;
drop index if exists product_driver_idx;
alter table product add column if not exists driver_name text;
update product x set driver_name = d.name from driver d where d.id = x.driver_id;
alter table product drop column driver_id;
alter table product rename column driver_name to driver_id;

alter table secret drop constraint if exists secret_secret_type_fkey;
alter table secret add column if not exists st_name text;
update secret x set st_name = t.name from secret_type t where t.id = x.secret_type;
alter table secret drop column secret_type;
alter table secret rename column st_name to secret_type;
alter table secret alter column secret_type set not null;

alter table location_type_property drop constraint if exists location_type_property_location_type_id_fkey;
alter table location_type_property drop constraint if exists location_type_property_location_type_id_property_id_key;
alter table location_type_property add column if not exists lt_name text;
update location_type_property x set lt_name = t.name from location_type t where t.id = x.location_type_id;
alter table location_type_property drop column location_type_id;
alter table location_type_property rename column lt_name to location_type_id;
alter table location_type_property alter column location_type_id set not null;

alter table location drop constraint if exists location_location_type_fkey;
alter table location add column if not exists lt_name text;
update location x set lt_name = t.name from location_type t where t.id = x.location_type;
alter table location drop column location_type;
alter table location rename column lt_name to location_type;
alter table location alter column location_type set not null;

alter table interface_type drop constraint if exists interface_type_pkey;
alter table interface_type drop constraint if exists interface_type_name_key;
alter table interface_type drop column id;
alter table interface_type add constraint interface_type_pkey primary key (name);

alter table driver drop constraint if exists driver_pkey;
alter table driver drop constraint if exists driver_name_key;
alter table driver drop column id;
alter table driver rename column name to id;
alter table driver add constraint driver_pkey primary key (id);

alter table secret_type drop constraint if exists secret_type_pkey;
alter table secret_type drop constraint if exists secret_type_name_key;
alter table secret_type drop column id;
alter table secret_type rename column name to id;
alter table secret_type add constraint secret_type_pkey primary key (id);

alter table location_type drop constraint if exists location_type_pkey;
alter table location_type drop constraint if exists location_type_name_key;
alter table location_type drop column id;
alter table location_type rename column name to id;
alter table location_type add constraint location_type_pkey primary key (id);

alter table location add constraint location_location_type_fkey
    foreign key (location_type) references location_type (id);
alter table location_type_property add constraint location_type_property_location_type_id_fkey
    foreign key (location_type_id) references location_type (id) on delete cascade;
alter table location_type_property add constraint location_type_property_location_type_id_property_id_key
    unique (location_type_id, property_id);
alter table secret add constraint secret_secret_type_fkey
    foreign key (secret_type) references secret_type (id);
alter table product add constraint product_driver_id_fkey
    foreign key (driver_id) references driver (id) on delete set null;
alter table interface add constraint interface_type_fkey
    foreign key (type) references interface_type (name);
