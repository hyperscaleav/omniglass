-- migrate:up

-- event is the sink for log-kind observations: a past occurrence on an estate
-- owner, as opposed to a metric_datapoint / state_datapoint (a sampled present
-- value). It shares the datapoint owner exclusive-arc and provenance vocabulary,
-- so the same reject-not-project and owner-confinement gates apply at ingest.
-- A log-kind datapoint carries a message (string_value) and/or structured
-- attributes (json_value); both land here instead of being dropped.
create table if not exists event (
    id                  bigint      generated always as identity primary key,
    ts                  timestamptz not null default now(),
    owner_kind          text        not null,
    component_id        text        references component (name) on delete cascade,
    system_id           text        references system (name) on delete cascade,
    location_id         text        references location (name) on delete cascade,
    node_id             text        references node (name) on delete cascade,
    key                 text        not null,
    instance            text        not null default '',
    message             text        not null default '',
    attributes          jsonb,
    provenance          text        not null default 'observed',
    source              text        not null default '',
    source_rule         text,
    source_rule_version bigint,
    constraint event_owner_kind_check check (owner_kind in ('component', 'system', 'location', 'node')),
    constraint event_provenance_check check (provenance in ('observed', 'calculated', 'intended', 'declared')),
    constraint event_owner_arc_check check (
           (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
        or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
        or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
        or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
    )
);
create index if not exists event_ts_brin on event using brin (ts);
create index if not exists event_owner_idx on event (component_id, key, instance, ts desc) where component_id is not null;

-- Close the reserved event_id stubs on the datapoint tables now that event
-- exists: an intended-provenance datapoint (a value set by an event) references
-- the event that produced it. Existing rows are all observed (event_id null), so
-- the constraint is satisfied without a backfill.
alter table metric_datapoint add constraint metric_datapoint_event_id_fkey
    foreign key (event_id) references event (id) on delete set null;
alter table state_datapoint add constraint state_datapoint_event_id_fkey
    foreign key (event_id) references event (id) on delete set null;

-- migrate:down

alter table state_datapoint drop constraint if exists state_datapoint_event_id_fkey;
alter table metric_datapoint drop constraint if exists metric_datapoint_event_id_fkey;
drop table if exists event;
