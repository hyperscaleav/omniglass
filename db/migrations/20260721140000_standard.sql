-- migrate:up

-- system_type is promoted into standard: the blueprint a system conforms to,
-- the system-side counterpart of product. It keeps its seeded rows (huddle-room,
-- meeting-room, and the rest are exactly the right vocabulary for a standard),
-- gains variant parenting like product.parent_product_id, and gains a declared
-- property contract (standard_property, below). Postgres has no RENAME IF EXISTS,
-- so the rename is guarded on information_schema to stay idempotent.
do $$
begin
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'system_type')
       and not exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'standard') then
        alter table system_type rename to standard;
    end if;
end $$;

-- rank was never read (the Go struct does not carry it); the registry lists by
-- display_name.
alter table standard drop column if exists rank;
alter table standard add column if not exists parent_standard_id text references standard (id) on delete set null;
alter table standard add column if not exists updated_at timestamptz not null default now();
create index if not exists standard_parent_idx on standard (parent_standard_id);

-- A system's blueprint becomes optional, matching component.product_id: a one-off
-- system simply conforms to no standard and carries only ad-hoc declared values.
do $$
begin
    if exists (select 1 from information_schema.columns
               where table_schema = 'public' and table_name = 'system' and column_name = 'system_type') then
        alter table system rename column system_type to standard_id;
    end if;
end $$;
alter table system alter column standard_id drop not null;

-- The two contract tables, mirroring product_property: which properties the
-- classifier declares and their defaults. data_type and validation are NOT
-- duplicated here, they belong to the property catalog.
create table if not exists standard_property (
    id            uuid        primary key default uuidv7(),
    standard_id   text        not null references standard (id) on delete cascade,
    property_name text        not null references property (name) on delete cascade,
    default_value jsonb,
    required      boolean     not null default false,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now(),
    unique (standard_id, property_name)
);
create index if not exists standard_property_property_idx on standard_property (property_name);

create table if not exists location_type_property (
    id               uuid        primary key default uuidv7(),
    location_type_id text        not null references location_type (id) on delete cascade,
    property_name    text        not null references property (name) on delete cascade,
    default_value    jsonb,
    required         boolean     not null default false,
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    unique (location_type_id, property_name)
);
create index if not exists location_type_property_property_idx on location_type_property (property_name);

-- migrate:down

drop table if exists location_type_property;
drop table if exists standard_property;
alter table system alter column standard_id set not null;
do $$
begin
    if exists (select 1 from information_schema.columns
               where table_schema = 'public' and table_name = 'system' and column_name = 'standard_id') then
        alter table system rename column standard_id to system_type;
    end if;
end $$;
drop index if exists standard_parent_idx;
alter table standard drop column if exists parent_standard_id;
alter table standard drop column if exists updated_at;
alter table standard add column if not exists rank integer not null default 0;
do $$
begin
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'standard')
       and not exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'system_type') then
        alter table standard rename to system_type;
    end if;
end $$;
