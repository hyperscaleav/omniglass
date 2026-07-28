-- migrate:up

-- ADR-0066 lineage: a derived event names what produced it. Rename the event's
-- causation column to source_event_id (the source is the cause, unifying the
-- former caused_by_event_id), and add source_log_line_id (the raw log line a rule
-- derived the event from) and derived_by_rule_id (which rule produced it). All
-- nullable: a natively-caught event has none. Postgres has no RENAME COLUMN IF
-- EXISTS, so the rename is guarded by a DO-block. The rule table lands with the
-- derivation engine, so derived_by_rule_id carries no FK yet.
do $$ begin
  if exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'event' and column_name = 'caused_by_event_id')
     and not exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'event' and column_name = 'source_event_id') then
    alter table event rename column caused_by_event_id to source_event_id;
  end if;
  if exists (select 1 from pg_constraint where conname = 'event_caused_by_event_id_fkey')
     and not exists (select 1 from pg_constraint where conname = 'event_source_event_id_fkey') then
    alter table event rename constraint event_caused_by_event_id_fkey to event_source_event_id_fkey;
  end if;
end $$;

alter table event add column if not exists source_log_line_id bigint;
alter table event add column if not exists derived_by_rule_id uuid;
do $$ begin
  if not exists (select 1 from pg_constraint where conname = 'event_source_log_line_id_fkey') then
    alter table event add constraint event_source_log_line_id_fkey foreign key (source_log_line_id) references log_line(id) on delete set null;
  end if;
end $$;

-- migrate:down

alter table event drop column if exists derived_by_rule_id;
alter table event drop constraint if exists event_source_log_line_id_fkey;
alter table event drop column if exists source_log_line_id;
do $$ begin
  if exists (select 1 from pg_constraint where conname = 'event_source_event_id_fkey')
     and not exists (select 1 from pg_constraint where conname = 'event_caused_by_event_id_fkey') then
    alter table event rename constraint event_source_event_id_fkey to event_caused_by_event_id_fkey;
  end if;
  if exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'event' and column_name = 'source_event_id')
     and not exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'event' and column_name = 'caused_by_event_id') then
    alter table event rename column source_event_id to caused_by_event_id;
  end if;
end $$;
