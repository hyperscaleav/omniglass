-- migrate:up

-- state_datapoint: the observed-state sink, the record substrate that makes
-- availability history honest. It mirrors metric_datapoint exactly (same owner
-- exclusive-arc, same lineage CHECK, same reject-not-project-is-app-level: no FK
-- on key) but its value is categorical text, not a number: a state has a value
-- DOMAIN, not a range. The first producer is the per-interface reachability
-- verdict interface.reachable (up/down), the AND of an interface's probe results,
-- computed and emitted by the node and routed here by the ingest consumer on the
-- datapoint_type kind. State is transition-only: one row per flip, not per tick
-- (node-side change detection + an ingest-side latest-value guard), so the
-- transition read reconstructs the availability strip. DDL is idempotent; owner
-- columns are the estate address (name), not the uuid.
create table if not exists state_datapoint (
    id                  bigint      generated always as identity primary key,
    ts                  timestamptz not null default now(),
    owner_kind          text        not null,
    component_id        text        references component (name) on delete cascade,
    system_id           text        references system (name) on delete cascade,
    location_id         text        references location (name) on delete cascade,
    node_id             text        references node (name) on delete cascade,
    key                 text        not null,
    instance            text        not null default '',
    value               text        not null,
    value_json          jsonb,
    provenance          text        not null default 'observed',
    source              text        not null default '',
    source_rule         text,
    source_rule_version bigint,
    event_id            bigint,
    audit_id            bigint,
    constraint state_datapoint_owner_kind_check check (owner_kind in ('component', 'system', 'location', 'node')),
    constraint state_datapoint_provenance_check check (provenance in ('observed', 'calculated', 'intended', 'declared')),
    constraint state_datapoint_owner_arc_check check (
           (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
        or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
        or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
        or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
    ),
    constraint state_datapoint_lineage_check check (
           (provenance = 'observed'   and event_id is null and audit_id is null)
        or (provenance = 'calculated' and source_rule is not null and event_id is null and audit_id is null)
        or (provenance = 'intended'   and event_id is not null and source_rule is null and audit_id is null)
        or (provenance = 'declared'   and audit_id is not null and source_rule is null and event_id is null)
    )
);
create index if not exists state_datapoint_ts_brin on state_datapoint using brin (ts);
create index if not exists state_datapoint_owner_idx on state_datapoint (component_id, key, instance, ts desc) where component_id is not null;

-- migrate:down

drop table if exists state_datapoint;
