-- migrate:up

-- capability and standard take uuid primary keys, and their kebab id becomes
-- `name`. Slice 2 of the registry epic; the ordering is slice 1's template: drop
-- every inbound foreign key first (a primary key cannot be dropped while one
-- depends on it), swap the keys, repoint the columns, re-add the constraints.
--
-- Eight inbound references: capability is named by product_capability,
-- role_capability, component_capability, and alarm_capability; standard by
-- product... no, by standard_property, system_role, and its own
-- parent_standard_id, plus system.standard_id.

-- Constraint names are not derivable here: `standard` was renamed from
-- `system_type` and `capability` predates several renames, so each kept the old
-- generated name. These look the real ones up instead of guessing.
create or replace function pg_temp.drop_pk(tbl text) returns void language plpgsql as $$
declare c text;
begin
  select conname into c from pg_constraint where contype = 'p' and conrelid = tbl::regclass;
  if c is not null then execute format('alter table %I drop constraint %I', tbl, c); end if;
end $$;

-- drop_refs drops every foreign key pointing AT a table, which is what has to
-- happen before its primary key can move.
create or replace function pg_temp.drop_refs(tbl text) returns void language plpgsql as $$
declare r record;
begin
  for r in select conrelid::regclass::text as t, conname from pg_constraint
            where contype = 'f' and confrelid = tbl::regclass loop
    execute format('alter table %s drop constraint %I', r.t, r.conname);
  end loop;
end $$;

select pg_temp.drop_refs('capability');
select pg_temp.drop_refs('standard');

alter table role_capability      drop constraint if exists role_capability_role_id_capability_id_key;
alter table component_capability drop constraint if exists component_capability_component_id_capability_id_key;
alter table alarm_capability     drop constraint if exists alarm_capability_alarm_id_capability_id_key;
alter table product_capability   drop constraint if exists product_capability_product_id_capability_id_key;
alter table standard_property    drop constraint if exists standard_property_standard_id_property_name_key;

-- capability -----------------------------------------------------------------
alter table capability rename column id to name;
alter table capability add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('capability');
alter table capability add constraint capability_pkey primary key (uuid_id);
alter table capability add constraint capability_name_key unique (name);

-- standard -------------------------------------------------------------------
alter table standard rename column id to name;
alter table standard add column if not exists uuid_id uuid not null default uuidv7();
select pg_temp.drop_pk('standard');
alter table standard add constraint standard_pkey primary key (uuid_id);
alter table standard add constraint standard_name_key unique (name);

-- repoint ---------------------------------------------------------------------
alter table product_capability add column if not exists cap_uuid uuid;
update product_capability x set cap_uuid = c.uuid_id from capability c where c.name = x.capability_id;
alter table product_capability drop column capability_id;
alter table product_capability rename column cap_uuid to capability_id;

alter table role_capability add column if not exists cap_uuid uuid;
update role_capability x set cap_uuid = c.uuid_id from capability c where c.name = x.capability_id;
alter table role_capability drop column capability_id;
alter table role_capability rename column cap_uuid to capability_id;

alter table component_capability add column if not exists cap_uuid uuid;
update component_capability x set cap_uuid = c.uuid_id from capability c where c.name = x.capability_id;
alter table component_capability drop column capability_id;
alter table component_capability rename column cap_uuid to capability_id;

alter table alarm_capability add column if not exists cap_uuid uuid;
update alarm_capability x set cap_uuid = c.uuid_id from capability c where c.name = x.capability_id;
alter table alarm_capability drop column capability_id;
alter table alarm_capability rename column cap_uuid to capability_id;

alter table standard_property add column if not exists std_uuid uuid;
update standard_property x set std_uuid = s.uuid_id from standard s where s.name = x.standard_id;
alter table standard_property drop column standard_id;
alter table standard_property rename column std_uuid to standard_id;

alter table system_role add column if not exists std_uuid uuid;
update system_role x set std_uuid = s.uuid_id from standard s where s.name = x.standard_id;
alter table system_role drop column standard_id;
alter table system_role rename column std_uuid to standard_id;

alter table standard add column if not exists parent_uuid uuid;
update standard x set parent_uuid = s.uuid_id from standard s where s.name = x.parent_standard_id;
alter table standard drop column parent_standard_id;
alter table standard rename column parent_uuid to parent_standard_id;

alter table system add column if not exists std_uuid uuid;
update system x set std_uuid = s.uuid_id from standard s where s.name = x.standard_id;
alter table system drop column standard_id;
alter table system rename column std_uuid to standard_id;

alter table capability rename column uuid_id to id;
alter table standard   rename column uuid_id to id;

alter table product_capability   alter column capability_id set not null;
alter table role_capability      alter column capability_id set not null;
alter table component_capability alter column capability_id set not null;
alter table alarm_capability     alter column capability_id set not null;
alter table standard_property    alter column standard_id   set not null;

alter table product_capability add constraint product_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table product_capability add constraint product_capability_product_id_capability_id_key
    unique (product_id, capability_id);
alter table role_capability add constraint role_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table role_capability add constraint role_capability_role_id_capability_id_key
    unique (role_id, capability_id);
alter table component_capability add constraint component_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table component_capability add constraint component_capability_component_id_capability_id_key
    unique (component_id, capability_id);
alter table alarm_capability add constraint alarm_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table alarm_capability add constraint alarm_capability_alarm_id_capability_id_key
    unique (alarm_id, capability_id);
alter table standard_property add constraint standard_property_standard_id_fkey
    foreign key (standard_id) references standard (id) on delete cascade;
alter table standard_property add constraint standard_property_standard_id_property_name_key
    unique (standard_id, property_name);
alter table system_role add constraint system_role_standard_id_fkey
    foreign key (standard_id) references standard (id) on delete cascade;
alter table system_role add constraint system_role_name_key
    unique nulls not distinct (owner_kind, standard_id, system_id, name);
create index if not exists system_role_standard_idx on system_role (standard_id) where standard_id is not null;
alter table standard add constraint standard_parent_standard_id_fkey
    foreign key (parent_standard_id) references standard (id) on delete set null;
alter table system add constraint system_standard_id_fkey
    foreign key (standard_id) references standard (id) on delete set null;

create index if not exists capability_name_idx on capability (name);
create index if not exists standard_name_idx on standard (name);

-- migrate:down

-- Constraint names are not derivable here: `standard` was renamed from
-- `system_type` and `capability` predates several renames, so each kept the old
-- generated name. These look the real ones up instead of guessing.
create or replace function pg_temp.drop_pk(tbl text) returns void language plpgsql as $$
declare c text;
begin
  select conname into c from pg_constraint where contype = 'p' and conrelid = tbl::regclass;
  if c is not null then execute format('alter table %I drop constraint %I', tbl, c); end if;
end $$;

-- drop_refs drops every foreign key pointing AT a table, which is what has to
-- happen before its primary key can move.
create or replace function pg_temp.drop_refs(tbl text) returns void language plpgsql as $$
declare r record;
begin
  for r in select conrelid::regclass::text as t, conname from pg_constraint
            where contype = 'f' and confrelid = tbl::regclass loop
    execute format('alter table %s drop constraint %I', r.t, r.conname);
  end loop;
end $$;

select pg_temp.drop_refs('capability');
select pg_temp.drop_refs('standard');


-- No release exists, so the reversal restores the shape rather than any operator
-- data beyond what the rows still carry: every reference points at a row that is
-- still there, so its current handle is the right answer.

alter table standard_property    drop constraint if exists standard_property_standard_id_property_name_key;
alter table alarm_capability     drop constraint if exists alarm_capability_alarm_id_capability_id_key;
alter table component_capability drop constraint if exists component_capability_capability_id_fkey;
alter table component_capability drop constraint if exists component_capability_component_id_capability_id_key;
alter table role_capability      drop constraint if exists role_capability_capability_id_fkey;
alter table role_capability      drop constraint if exists role_capability_role_id_capability_id_key;
alter table product_capability   drop constraint if exists product_capability_capability_id_fkey;
alter table product_capability   drop constraint if exists product_capability_product_id_capability_id_key;

alter table system add column if not exists std_name text;
update system x set std_name = s.name from standard s where s.id = x.standard_id;
alter table system drop column standard_id;
alter table system rename column std_name to standard_id;

alter table standard add column if not exists parent_name text;
update standard x set parent_name = s.name from standard s where s.id = x.parent_standard_id;
alter table standard drop column parent_standard_id;
alter table standard rename column parent_name to parent_standard_id;

alter table system_role add column if not exists std_name text;
update system_role x set std_name = s.name from standard s where s.id = x.standard_id;
alter table system_role drop column standard_id;
alter table system_role rename column std_name to standard_id;

alter table standard_property add column if not exists std_name text;
update standard_property x set std_name = s.name from standard s where s.id = x.standard_id;
alter table standard_property drop column standard_id;
alter table standard_property rename column std_name to standard_id;

alter table alarm_capability add column if not exists cap_name text;
update alarm_capability x set cap_name = c.name from capability c where c.id = x.capability_id;
alter table alarm_capability drop column capability_id;
alter table alarm_capability rename column cap_name to capability_id;

alter table component_capability add column if not exists cap_name text;
update component_capability x set cap_name = c.name from capability c where c.id = x.capability_id;
alter table component_capability drop column capability_id;
alter table component_capability rename column cap_name to capability_id;

alter table role_capability add column if not exists cap_name text;
update role_capability x set cap_name = c.name from capability c where c.id = x.capability_id;
alter table role_capability drop column capability_id;
alter table role_capability rename column cap_name to capability_id;

alter table product_capability add column if not exists cap_name text;
update product_capability x set cap_name = c.name from capability c where c.id = x.capability_id;
alter table product_capability drop column capability_id;
alter table product_capability rename column cap_name to capability_id;

select pg_temp.drop_pk('standard');
alter table standard   drop constraint if exists standard_name_key;
alter table standard   drop column id;
alter table standard   rename column name to id;
alter table standard   add constraint standard_pkey primary key (id);

select pg_temp.drop_pk('capability');
alter table capability drop constraint if exists capability_name_key;
alter table capability drop column id;
alter table capability rename column name to id;
alter table capability add constraint capability_pkey primary key (id);

alter table product_capability add constraint product_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table product_capability add constraint product_capability_product_id_capability_id_key
    unique (product_id, capability_id);
alter table role_capability add constraint role_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table role_capability add constraint role_capability_role_id_capability_id_key
    unique (role_id, capability_id);
alter table component_capability add constraint component_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table component_capability add constraint component_capability_component_id_capability_id_key
    unique (component_id, capability_id);
alter table alarm_capability add constraint alarm_capability_capability_id_fkey
    foreign key (capability_id) references capability (id) on delete cascade;
alter table alarm_capability add constraint alarm_capability_alarm_id_capability_id_key
    unique (alarm_id, capability_id);
alter table standard_property add constraint standard_property_standard_id_fkey
    foreign key (standard_id) references standard (id) on delete cascade;
alter table standard_property add constraint standard_property_standard_id_property_name_key
    unique (standard_id, property_name);
alter table system_role add constraint system_role_standard_id_fkey
    foreign key (standard_id) references standard (id) on delete cascade;
alter table standard add constraint standard_parent_standard_id_fkey
    foreign key (parent_standard_id) references standard (id) on delete set null;
alter table system add constraint system_standard_id_fkey
    foreign key (standard_id) references standard (id) on delete set null;
