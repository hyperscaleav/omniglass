-- migrate:up

-- The collection tier: a node (the edge runtime), the interface_type and
-- datapoint_type registries, an interface (a placement-bound connection), a
-- task (a node's content-addressed unit of work), and metric_datapoint (the
-- observed-metric sink). DDL is idempotent. Enums are text + CHECK. A datapoint
-- owner is addressed by its estate name, not a uuid; the interface and task each
-- carry their own surrogate uuid key (an interface name is unique only within its
-- component). reject-not-project is enforced in the app, so metric_datapoint.key
-- has no FK to datapoint_type.

-- The edge runtime is a first-class principal of kind='node' (alongside human
-- and service), so this is its 1:1 per-kind detail table keyed by principal_id.
-- name is the estate address the collection FKs reference (not null unique), so
-- interface/task/metric_datapoint keep resolving a node by name. The enrollment
-- secret is a bearer credential ROW on the principal (see internal/storage), not
-- a column here. enrolled_at is stamped on the first claim.
--
-- name is also a NATS subject token (og.v1.telemetry.<name>, og.v1.worklist.<name>,
-- etc.) and the node's per-node subject grant, so the CHECK below rejects the
-- characters that would break the subject model or forge a wildcard grant: a dot
-- (token separator), '*'/'>' (subject wildcards), and whitespace. This mirrors
-- validNodeName in internal/api/nodes.go; the API layer stays belt-and-suspenders,
-- but the Storage Gateway is the enforcement boundary.
create table if not exists node (
    principal_id      uuid        primary key references principal (id) on delete cascade,
    name              text        not null unique,
    description       text        not null default '',
    last_heartbeat_at timestamptz,
    enrolled_at       timestamptz,
    labels            jsonb       not null default '{}'::jsonb,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now(),
    constraint node_name_subject_safe_check check (name ~ '^[^.*> \t\n\r]+$')
);

-- The interface_type registry: which connection kinds exist and which have a
-- built adapter. Mirrors component_type (official flag), plus a built flag.
create table if not exists interface_type (
    name        text        primary key,
    official    boolean     not null default false,
    description text        not null default '',
    built       boolean     not null default false,
    created_at  timestamptz not null default now()
);

-- The datapoint_type registry: the governed measurement vocabulary. scope
-- decides where the name is unique (official/org, or per template). template_id
-- is null except at scope=template (no FK yet: the template table lands later).
-- unit/precision are metric-only; validation is {min,max} for metric,
-- {values:[...]} for state.
create table if not exists datapoint_type (
    scope         text        not null default 'official',
    name          text        not null,
    template_id   uuid,
    display_name  text,
    kind          text        not null,
    value_type    text        not null,
    unit          text,
    precision     integer,
    fusion_policy jsonb,
    validation    jsonb,
    description   text        not null default '',
    registered_at timestamptz not null default now(),
    primary key (scope, name),
    constraint datapoint_type_scope_check      check (scope in ('official', 'org', 'template')),
    constraint datapoint_type_kind_check       check (kind in ('metric', 'state', 'log')),
    constraint datapoint_type_value_type_check check (value_type in ('int', 'float', 'text', 'json'))
);

-- An interface: a named, placement-bound connection. id is the surrogate address
-- (a uuidv7, so a friendly name can be reused across components and renamed later
-- without breaking a task's reference); name is unique WITHIN its owning component
-- (unique(component, name)), so operators address it by a friendly name. type is an
-- interface_type; component is the owner (nullable for a server-hosted interface,
-- which the unique tuple treats as distinct per NULL, so it is not constrained);
-- node_name is the server-assigned placement; params holds the endpoint/target and
-- settings.
create table if not exists interface (
    id         uuid        primary key default uuidv7(),
    name       text        not null,
    type       text        not null references interface_type (name),
    component  text        references component (name) on delete set null,
    node_name  text        references node (name) on delete cascade,
    params     jsonb       not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint interface_component_name_key unique (component, name)
);

-- A task: a node's content-addressed unit of collection work. mode is the
-- poll/listen axis; spec holds the inline probe type and params. id is a content
-- hash so identical work dedupes. interface_id references the interface's surrogate
-- id; on delete restrict, so an interface with tasks cannot be dropped.
create table if not exists task (
    id             text        primary key,
    display_name   text        not null default '',
    mode           text        not null,
    interface_id   uuid        not null references interface (id) on delete cascade,
    spec           jsonb       not null default '{}'::jsonb,
    enabled        boolean     not null default true,
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now(),
    constraint task_mode_check check (mode in ('poll', 'listen'))
);
create index if not exists task_interface_idx on task (interface_id);
-- A task's node is a projection of its interface's placement (interface.node_name),
-- not a column: the node derives its worklist by joining task to interface. This
-- partial index on placed interfaces backs that worklist join.
create index if not exists interface_node_name_idx on interface (node_name) where node_name is not null;

-- metric_datapoint: the observed-metric sink. Owner is exactly one of the four
-- estate/edge arms (or, later, none for a global singleton). The lineage CHECK
-- is the pragmatic 4-arm form: observed carries no rule (the direct-placement
-- path binds owner from the task, not a transform_rule), source_rule is required
-- only for calculated, intended points to an event, declared to an audit row.
-- event_id/audit_id are reserved columns with no FK yet (their tables land in
-- later slices). Not partitioned in slice 1 (low volume); BRIN on ts plus a
-- per-owner series index. Owner columns are the estate address (name).
create table if not exists metric_datapoint (
    id                  bigint           generated always as identity primary key,
    ts                  timestamptz      not null default now(),
    owner_kind          text             not null,
    component_id        text             references component (name) on delete cascade,
    system_id           text             references system (name) on delete cascade,
    location_id         text             references location (name) on delete cascade,
    node_id             text             references node (name) on delete cascade,
    key                 text             not null,
    instance            text             not null default '',
    value               double precision not null,
    provenance          text             not null default 'observed',
    source              text             not null default '',
    source_rule         text,
    source_rule_version bigint,
    event_id            bigint,
    audit_id            bigint,
    constraint metric_datapoint_owner_kind_check check (owner_kind in ('component', 'system', 'location', 'node')),
    constraint metric_datapoint_provenance_check check (provenance in ('observed', 'calculated', 'intended', 'declared')),
    constraint metric_datapoint_owner_arc_check check (
           (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
        or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
        or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
        or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
    ),
    constraint metric_datapoint_lineage_check check (
           (provenance = 'observed'   and event_id is null and audit_id is null)
        or (provenance = 'calculated' and source_rule is not null and event_id is null and audit_id is null)
        or (provenance = 'intended'   and event_id is not null and source_rule is null and audit_id is null)
        or (provenance = 'declared'   and audit_id is not null and source_rule is null and event_id is null)
    )
);
create index if not exists metric_datapoint_ts_brin on metric_datapoint using brin (ts);
create index if not exists metric_datapoint_owner_idx on metric_datapoint (component_id, key, instance, ts desc) where component_id is not null;

-- migrate:down

drop table if exists metric_datapoint;
drop table if exists task;
drop table if exists interface;
drop table if exists datapoint_type;
drop table if exists interface_type;
drop table if exists node;
