-- migrate:up

-- The catalog splits into two lanes (#587): metric_type carries the numeric
-- facts (unit, precision), property_type keeps the value domain (validation).
-- The partition key is data_type, because the lane IS the value type: metrics
-- are numbers, properties are everything else. kind retires with the split; it
-- was the old two-in-one table's discriminator and its one functional reader
-- (the ingest observable-gate) moves to catalog membership.
--
-- Trap, named because it already bit once: after the July renames, `property`
-- is the VALUE STORE and `property_type` is the catalog, so a bare
-- REFERENCES property(id) is valid DDL that points at the wrong table. Every
-- reference below names its target explicitly.
--
-- Moved rows keep their id, so sample and contract FKs repoint as a pure
-- constraint swap and nothing orphans.

create table if not exists metric_type (
    name text not null,
    display_name text,
    data_type text not null,
    unit text,
    "precision" integer,
    fusion_policy jsonb,
    description text default ''::text not null,
    registered_at timestamptz default now() not null,
    official boolean default false not null,
    id uuid default uuidv7() not null
);
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'metric_type_pkey') then
    alter table metric_type add constraint metric_type_pkey primary key (id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'metric_type_handle_key') then
    alter table metric_type add constraint metric_type_handle_key unique (name);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'metric_type_data_type_check') then
    alter table metric_type add constraint metric_type_data_type_check
      check (data_type = any (array['int'::text, 'float'::text]));
  end if;
end $$;

-- The per-domain contract tables. A contract may declare a metric (a product
-- ships a default sample-rate; a standard requires a seat count), so each
-- contract family gains a metric sibling mirroring its property shape.
create table if not exists product_metric (
    id uuid default uuidv7() not null,
    default_value jsonb,
    required boolean default false not null,
    created_at timestamptz default now() not null,
    updated_at timestamptz default now() not null,
    product_id uuid not null,
    metric_type_id uuid not null
);
create table if not exists standard_metric (
    id uuid default uuidv7() not null,
    default_value jsonb,
    required boolean default false not null,
    created_at timestamptz default now() not null,
    updated_at timestamptz default now() not null,
    standard_id uuid not null,
    metric_type_id uuid not null
);
create table if not exists location_type_metric (
    id uuid default uuidv7() not null,
    default_value jsonb,
    required boolean default false not null,
    created_at timestamptz default now() not null,
    updated_at timestamptz default now() not null,
    location_type_id uuid not null,
    metric_type_id uuid not null
);
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'product_metric_pkey') then
    alter table product_metric add constraint product_metric_pkey primary key (id);
    alter table product_metric add constraint product_metric_product_id_metric_type_id_key
      unique (product_id, metric_type_id);
    alter table product_metric add constraint product_metric_product_id_fkey
      foreign key (product_id) references product(id) on delete cascade;
    alter table product_metric add constraint product_metric_metric_type_id_fkey
      foreign key (metric_type_id) references metric_type(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'standard_metric_pkey') then
    alter table standard_metric add constraint standard_metric_pkey primary key (id);
    alter table standard_metric add constraint standard_metric_standard_id_metric_type_id_key
      unique (standard_id, metric_type_id);
    alter table standard_metric add constraint standard_metric_standard_id_fkey
      foreign key (standard_id) references standard(id) on delete cascade;
    alter table standard_metric add constraint standard_metric_metric_type_id_fkey
      foreign key (metric_type_id) references metric_type(id) on delete cascade;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'location_type_metric_pkey') then
    alter table location_type_metric add constraint location_type_metric_pkey primary key (id);
    alter table location_type_metric add constraint location_type_metric_location_type_id_metric_type_id_key
      unique (location_type_id, metric_type_id);
    alter table location_type_metric add constraint location_type_metric_location_type_id_fkey
      foreign key (location_type_id) references location_type(id) on delete cascade;
    alter table location_type_metric add constraint location_type_metric_metric_type_id_fkey
      foreign key (metric_type_id) references metric_type(id) on delete cascade;
  end if;
end $$;

-- Copy the numeric rows across, ids preserved. Idempotent: a re-run finds them
-- already present and does nothing.
insert into metric_type (id, name, display_name, data_type, unit, "precision",
                         fusion_policy, description, registered_at, official)
select id, name, display_name, data_type, unit, "precision",
       fusion_policy, description, registered_at, official
from property_type
where data_type in ('int', 'float')
on conflict (id) do nothing;

-- Refuse rather than guess on the two references that cannot follow the split
-- mechanically. Pre-release this raises on nothing; on a hypothetical estate it
-- names the row instead of breaking an FK mid-migration.
do $$
declare
  offender text;
begin
  -- A value-store row holding a declared value of a type that just became a
  -- metric. Slice F folds these into the series; until then they block.
  select pt.name into offender
  from property p join property_type pt on pt.id = p.property_type_id
  where pt.data_type in ('int', 'float') limit 1;
  if offender is not null then
    raise exception 'value store holds a declared value of %, which is now a metric type; fold it before splitting', offender;
  end if;
  -- A command targeting a type that moved. The two-armed target arc lands in a
  -- later slice; until then a moved target would dangle.
  select pt.name into offender
  from command_type ct join property_type pt on pt.id = ct.target_property_type_id
  where pt.data_type in ('int', 'float') limit 1;
  if offender is not null then
    raise exception 'command_type targets %, which is now a metric type; the target arc lands in a later slice', offender;
  end if;
end $$;

-- Contract rows whose declared type moved lanes move with it, ids preserved.
insert into product_metric (id, default_value, required, created_at, updated_at, product_id, metric_type_id)
select pp.id, pp.default_value, pp.required, pp.created_at, pp.updated_at, pp.product_id, pp.property_type_id
from product_property pp join metric_type mt on mt.id = pp.property_type_id
on conflict (id) do nothing;
delete from product_property pp using metric_type mt where mt.id = pp.property_type_id;

insert into standard_metric (id, default_value, required, created_at, updated_at, standard_id, metric_type_id)
select sp.id, sp.default_value, sp.required, sp.created_at, sp.updated_at, sp.standard_id, sp.property_type_id
from standard_property sp join metric_type mt on mt.id = sp.property_type_id
on conflict (id) do nothing;
delete from standard_property sp using metric_type mt where mt.id = sp.property_type_id;

insert into location_type_metric (id, default_value, required, created_at, updated_at, location_type_id, metric_type_id)
select lp.id, lp.default_value, lp.required, lp.created_at, lp.updated_at, lp.location_type_id, lp.property_type_id
from location_type_property lp join metric_type mt on mt.id = lp.property_type_id
on conflict (id) do nothing;
delete from location_type_property lp using metric_type mt where mt.id = lp.property_type_id;

-- The metric sample table repoints at its own catalog: drop the old FK, rename
-- the column (PG has no RENAME COLUMN IF EXISTS, hence the DO-block), add the
-- new FK. Every metric sample references a numeric type by construction, and
-- the insert above carried every numeric type across, so the new FK validates.
do $$ begin
  if exists (select 1 from pg_constraint where conname = 'metric_property_type_id_fkey') then
    alter table metric drop constraint metric_property_type_id_fkey;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'metric' and column_name = 'property_type_id')
     and not exists (select 1 from information_schema.columns where table_name = 'metric' and column_name = 'metric_type_id') then
    alter table metric rename column property_type_id to metric_type_id;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'metric_metric_type_id_fkey') then
    alter table metric add constraint metric_metric_type_id_fkey
      foreign key (metric_type_id) references metric_type(id) on delete cascade;
  end if;
end $$;

-- With every reference repointed, the moved rows leave the property catalog,
-- and the catalog narrows to its lane: no kind, no unit, no precision, and a
-- data_type domain of the non-numeric encodings.
delete from property_type pt using metric_type mt where mt.id = pt.id;

do $$ begin
  if exists (select 1 from pg_constraint where conname = 'property_type_kind_check') then
    alter table property_type drop constraint property_type_kind_check;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'property_type' and column_name = 'kind') then
    alter table property_type drop column kind;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'property_type' and column_name = 'unit') then
    alter table property_type drop column unit;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'property_type' and column_name = 'precision') then
    alter table property_type drop column "precision";
  end if;
  if exists (select 1 from pg_constraint where conname = 'property_type_data_type_check') then
    alter table property_type drop constraint property_type_data_type_check;
  end if;
  alter table property_type add constraint property_type_data_type_check
    check (data_type = any (array['string'::text, 'bool'::text, 'json'::text]));
end $$;

-- Nothing lost: the two lanes together hold every row the one catalog held.
-- (The counts are checked as disjointness plus FK validity, because the
-- pre-split total is not observable from inside the migration on a re-run.)
do $$
declare
  n bigint;
begin
  select count(*) into n from metric_type mt join property_type pt on pt.id = mt.id;
  if n > 0 then
    raise exception 'catalog split left % row(s) in both lanes', n;
  end if;
  select count(*) into n from metric m left join metric_type mt on mt.id = m.metric_type_id where mt.id is null;
  if n > 0 then
    raise exception 'catalog split orphaned % metric sample(s)', n;
  end if;
end $$;

-- migrate:down

-- The reverse fold: the catalog becomes one table again. Moved rows kept their
-- ids in both directions, so the fold-back is insert-by-id with the numeric
-- facts (unit, precision) carried and kind restored per lane; nothing is
-- guessed and nothing orphans. The pre-split checks come back in the form the
-- up found them (the #395 tightening had already dropped 'log' from kind).

-- The wide columns first, and the wide data_type domain BEFORE the numeric
-- rows return (an int row cannot enter under the narrowed check).
alter table property_type add column if not exists kind text;
alter table property_type add column if not exists unit text;
alter table property_type add column if not exists "precision" integer;
update property_type set kind = 'state' where kind is null;
alter table property_type drop constraint if exists property_type_data_type_check;
alter table property_type add constraint property_type_data_type_check
    check (data_type = any (array['string'::text, 'int'::text, 'float'::text, 'bool'::text, 'json'::text]));
alter table property_type drop constraint if exists property_type_kind_check;
alter table property_type add constraint property_type_kind_check
    check (kind = any (array['metric'::text, 'state'::text]));

-- The numeric rows fold back, ids preserved, kind='metric'. Idempotent: a
-- re-run finds them present and does nothing.
insert into property_type (id, name, display_name, data_type, unit, "precision",
                           fusion_policy, description, registered_at, official, kind)
select id, name, display_name, data_type, unit, "precision",
       fusion_policy, description, registered_at, official, 'metric'
from metric_type
on conflict (id) do nothing;

-- Contract rows return to their property siblings, ids preserved.
insert into product_property (id, default_value, required, created_at, updated_at, product_id, property_type_id)
select id, default_value, required, created_at, updated_at, product_id, metric_type_id
from product_metric
on conflict (id) do nothing;

insert into standard_property (id, default_value, required, created_at, updated_at, standard_id, property_type_id)
select id, default_value, required, created_at, updated_at, standard_id, metric_type_id
from standard_metric
on conflict (id) do nothing;

insert into location_type_property (id, default_value, required, created_at, updated_at, location_type_id, property_type_id)
select id, default_value, required, created_at, updated_at, location_type_id, metric_type_id
from location_type_metric
on conflict (id) do nothing;

-- The metric sample table repoints back at the one catalog: drop the lane FK,
-- rename the column back, restore the old FK name. The fold-back above carried
-- every numeric type across, so the FK validates.
do $$ begin
  if exists (select 1 from pg_constraint where conname = 'metric_metric_type_id_fkey') then
    alter table metric drop constraint metric_metric_type_id_fkey;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'metric' and column_name = 'metric_type_id')
     and not exists (select 1 from information_schema.columns where table_name = 'metric' and column_name = 'property_type_id') then
    alter table metric rename column metric_type_id to property_type_id;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'metric_property_type_id_fkey') then
    alter table metric add constraint metric_property_type_id_fkey
      foreign key (property_type_id) references property_type(id) on delete cascade;
  end if;
end $$;

-- With every reference repointed, the lane tables go.
drop table if exists product_metric;
drop table if exists standard_metric;
drop table if exists location_type_metric;
drop table if exists metric_type;
