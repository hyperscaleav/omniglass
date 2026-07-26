-- migrate:up

-- ADR-0063 #395: events get their own registry. event_type is the occurrence-key
-- registry (the twin of property_type); event is repointed off property_type
-- (kind=log) onto event_type and gains origin/causation/correlation. The log kind
-- then leaves property_type. Event rows are ephemeral telemetry logs (prod is
-- pre-release and dev re-derives), so the repoint clears the occurrence log rather
-- than backfilling a type (epic ADR-0063 Decision C).

create table if not exists event_type (
    id uuid default uuidv7() not null,
    name text not null,
    display_name text,
    description text default ''::text not null,
    payload_schema jsonb,
    official boolean default false not null,
    registered_at timestamptz default now() not null
);
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'event_type_pkey') then
    alter table event_type add constraint event_type_pkey primary key (id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'event_type_name_key') then
    alter table event_type add constraint event_type_name_key unique (name);
  end if;
end $$;

-- Clear the ephemeral occurrence log before the type repoint (no backfill).
delete from event;

alter table event add column if not exists event_type_id uuid;
alter table event add column if not exists origin text default 'caught'::text not null;
alter table event add column if not exists caused_by_event_id bigint;
alter table event add column if not exists correlation_id text;

do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'event_origin_check') then
    alter table event add constraint event_origin_check
      check (origin = any (array['caught'::text, 'caused'::text, 'derived'::text, 'scheduled'::text]));
  end if;
  if not exists (select 1 from pg_constraint where conname = 'event_event_type_id_fkey') then
    alter table event add constraint event_event_type_id_fkey foreign key (event_type_id) references event_type(id);
  end if;
  if not exists (select 1 from pg_constraint where conname = 'event_caused_by_event_id_fkey') then
    alter table event add constraint event_caused_by_event_id_fkey foreign key (caused_by_event_id) references event(id);
  end if;
end $$;

-- Drop the old property_type FK + column; event_type now carries the type.
alter table event drop constraint if exists event_property_type_id_fkey;
alter table event drop column if exists property_type_id;
alter table event alter column event_type_id set not null;

-- The log kind leaves property_type: drop the log-kind rows (nothing references
-- them now), then tighten the kind check to metric/state.
delete from property_type where kind = 'log';
alter table property_type drop constraint if exists property_type_kind_check;
alter table property_type add constraint property_type_kind_check
  check (kind = any (array['metric'::text, 'state'::text]));

-- migrate:down

alter table property_type drop constraint if exists property_type_kind_check;
alter table property_type add constraint property_type_kind_check
  check (kind = any (array['metric'::text, 'state'::text, 'log'::text]));

delete from event;
alter table event add column if not exists property_type_id uuid;
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'event_property_type_id_fkey') then
    alter table event add constraint event_property_type_id_fkey foreign key (property_type_id) references property_type(id);
  end if;
end $$;

alter table event drop constraint if exists event_caused_by_event_id_fkey;
alter table event drop constraint if exists event_event_type_id_fkey;
alter table event drop constraint if exists event_origin_check;
alter table event drop column if exists correlation_id;
alter table event drop column if exists caused_by_event_id;
alter table event drop column if exists origin;
alter table event drop column if exists event_type_id;

drop table if exists event_type;
