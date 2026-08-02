---
title: Storage
description: "How storage works: the Storage Gateway, views by default, per-database isolation, append-only partitioning and tiering, and the on-row lineage pattern."
sidebar:
  badge:
    text: Partial
    variant: note
---

Storage is the set of patterns every entity in Omniglass lands on, so an operator can trust that scope, audit, retention, and lineage behave the same way no matter which table the data lives in. This page describes **how storage works**, the
patterns every other leaf's entities land on, not a per-table column dump.

:::note[Partial]
Built today: the Storage Gateway as the only door to the database, dbmate migrations (run-once,
embedded, idempotent), the per-action scope predicate and the in-transaction `audit_log` write, and the
shared scoped-tree and scoped-CRUD primitives. Scope arrives at the gateway as an explicit per-call
`scope.Set` argument; seed and system callers pass an all-scope set. Today `metric` / `state` carry no
`series` column and no uniqueness key, so redelivery can duplicate
([#430](https://github.com/hyperscaleav/omniglass/issues/430) stage 3). The design fences below mark
the parts of this page that are target design, each naming its tracker. The `property` current-value
cache is built, a table upserted from the sink, not the metric-view once sketched here
([properties](/architecture/properties/#the-current-value-cache)).
See [implementation status](/architecture/status/).
:::

Postgres is the **relational system of record**: it holds the entities, events, alarms, actions,
audit, config, and the platform settings store. It is the record/state/intent lane. It is **never a
message bus**: the live signal travels on NATS JetStream, and Postgres earns its place as the durable
record. Two writes paths land here, and only one is the request path. **Operator mutations and the
record/state/intent lane** (config, ack/snooze, settings, manual commands) are written synchronously
through the Storage Gateway.

:::design[Target design, tracked in #430]
**The sample tables are an async SINK**: a NATS **persistence consumer** batch-writes samples off the
data lane ([samples](/architecture/properties/)), idempotent on `(series, ts)`, so the rule engine
never waits on a sample reaching Postgres. Committed changes on the record lane (including the `event`
and `alarm` rows an `event_rule` consumer commits in one transaction) are fanned out by a
leader-elected **CDC publisher** (logical decoding of the WAL) to JetStream; there is no dual-write,
the change is born in the commit and CDC carries it.
:::

The column schemas live
with each owning feature: [samples](/architecture/properties/#the-sample-tables) (the three
kind-tables), [events](/architecture/events/#storage) (the `event` row), [alarms and
actions](/architecture/alarms-actions/#storage) (`alarm` / `action`),
[commands](/architecture/commands/) (the command pillar: the `command_type` registry, the `command`
invocation row, and its computed settlement), [config and
credentials](/architecture/variables/#storage) (`variable` / config / tags), [core
entities](/architecture/core-entities/) and [templates](/architecture/templates/) (the structural and
template tables), [collection](/architecture/collection/#storage) (interfaces and tasks),
[calculations](/architecture/calculations/#storage) (the rule families), [files](/architecture/files/),
[time](/architecture/time/#storage), and [identity and access](/architecture/identity-access/#storage).

## Conventions

- **No `tenant_id`.** Isolation is per-database (a database per tenant); there is no tenant column
  anywhere. The registries and catalogs (`property_type`, `event_type`, `interface_type`,
  `location_type`, `secret_type`, `vendor`, `driver`, `capability`, `product`, `standard`) carry an
  **`official` boolean** (a richer per-registry `scope` ladder, template / org / official, remains
  future design; see [key scope](/architecture/properties/#key-scope-template-org-official)):
  `official: true` rows are the
  ship-with canonical set distributed with the binary, and `official: false` rows are operator- or
  org-authored, local to this deployment. The boolean is about **authority, not provenance**: a row
  the binary seeds is not automatically `official`. A `standard` and a `location_type` ship as
  `official: false` and are installed **only if absent**, because they are example content an estate
  owns and edits; the canonical catalogs (`property` above all) ship `official: true` through an
  authoritative `ON CONFLICT DO UPDATE`, so a release can correct the shared vocabulary
  ([the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)).
- **Three storage shapes.** **Ground-truth records** are append-only and immutable, each named for
  what it is: `log_line` (the raw log lane, not a sample), `audit_log` (operator actions), and the standing
  `*_log` ground-truth logs (`session_log`, `internal_log`, plus the `collection_log` /
  `node_log` companions; [ground-truth records](#ground-truth-records) below). There is **no `telemetry` table**: samples are published to the
  JetStream data lane, not synchronously inserted, so the raw payload is not persisted in steady
  state; the persistence consumer sinks the typed sample, and raw appears only on a
  `collection.failed` event or a dev raw-mode tap ([samples](/architecture/properties/)). A
  schedule fire is not a record here: it is an `event` with `origin=scheduled`.
  There is no separate rule-execution table: derived rows carry their lineage on the row.
  **Samples** (`metric` / `state`) are the typed
  observation firehose, and `log_line` is the untyped raw arrival beside them
  ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)): a log line is
  not a sample kind, so it carries no property name and passes no registry gate. **Stateful entities and projections** (`alarm`, `action`, current-value)
  hold state directly or are rebuildable read models, **views by default**. The model is **not
  event-sourced**.
- **Provenance and lineage on every sample**: `provenance` (observed / calculated / intended /
  declared), `source` (which sensor or path, for observed), and a lineage pointer. **calculated**
  requires `source_rule` (+ version), the rule that produced the row; **intended** requires `event_id`
  (the command) and no `source_rule`; **observed** requires only that `event_id` is null (it may carry
  a `source_rule` when an edge function produced it, but the CHECK does not demand one); **declared**
  requires neither pointer. A CHECK enforces the pointer per provenance, and **observed vs calculated
  is the `provenance` value itself**, not a column-presence trick. `declared` exists as a sample
  provenance in the schema ([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)),
  although the model going forward keeps declared config in [config](/architecture/variables/), keyed
  to the same signal.
- **Ownership is the exclusive-arc**, though not one uniform arc. The sample tables (`metric` /
  `state`) and `event` carry `owner_kind` (`component` / `system` / `location` / `node`) plus the
  matching typed FK and a CHECK that exactly the matching column is set; there is no platform or
  global arm on a sample. `variable`'s arc is `platform` / `component` / `system` / `location` (no
  node arm; the `platform` kind sets all three FKs null). `alarm` carries **no arc**: a single NOT
  NULL `component_id`, component-local by design today.
  The full pattern is on [core entities](/architecture/core-entities/#ownership-the-exclusive-arc).
- **A write struct takes the `Write` suffix; the bare noun is the row.** `MetricSampleWrite` is what a
  caller hands the gateway, `MetricSample` is what comes back, and the same holds for `StateSampleWrite`,
  `EventWrite`, and `LogLineWrite`. A carrier is likewise named for what it carries rather than for one of
  its passengers, which is why the telemetry wire message is a `TelemetryBatch`
  ([ADR-0072](/architecture/decisions/#adr-0072-an-envelope-is-not-named-after-its-passengers-and-an-insert-struct-takes-the-write-suffix)).
- **Keys**: samples and events use a surrogate id plus `ts`; the key registry `property_type` is
  name-unique with the **`official` boolean** deciding authority (the template / org / official
  `scope` ladder is future design); structural entities are name-keyed; a `task` is **content-addressed**
  (`sha256` over `(interface_id, mode, spec)`); a `node` by its `principal_id`, its enrollment
  identity. Every foreign key stores the target's primary key, so a rename is free
  ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)).

## How the records relate

The relationships, not the columns. The columns of each table live on its owning leaf (linked above).

```d2
direction: right
classes: { node: { style.border-radius: 8 } }
metric: metric { class: node }
state: state { class: node }
event: event { class: node }
alarm: alarm { class: node }
action: action { class: node }
property: property { class: node }
variable: variable { class: node }
metric -> metric: calc_rule
state -> event: event_rule
event -> alarm: fire opens · clear resolves
event -> action: action_rule
alarm -> action
metric -> property: current value
state -> property: current value
state -> variable: linked_state (observed side)
```

The structural and template entities (`component` / `system` / `location` and the `*_template` /
`product` / `standard` / `system_role` / `system_member` families) relate as shown on
[core entities](/architecture/core-entities/) and [templates](/architecture/templates/); the
collection entities (`interface_type` / `interface` / `task`) on
[collection](/architecture/collection/#storage).

:::design[Target design, tracked in #430]

## Two lanes land in Postgres differently

Every row in Postgres arrives on one of two lanes, and the lane decides how the row is written and
how the rest of the platform learns it changed.

- **The data lane (a sink).** Observed and calculated samples live on the JetStream data lane.
  The rule engine consumes them directly off NATS; Postgres is the durable record, not the live
  signal. The **persistence consumer** is a durable JetStream consumer that batch-writes the
  `metric` / `state` tables as an async sink, idempotent on
  `(series, ts)` ([#430](https://github.com/hyperscaleav/omniglass/issues/430) stage 3), so a
  redelivery lands the same row and the firehose never blocks on the database.
  Raw log lines ride the same ingest path into `log_line`, but keyed on nothing (an untyped arrival has
  no series), so their at-least-once story is the lane's own, not this one's.
  Samples do **not** flow through CDC: they are already on NATS.
- **The record/state/intent lane (PG-first, CDC-out).** Events, alarms, actions, and operator
  mutations (config, ack/snooze, settings, manual commands) are born in a **Postgres transaction**.
  When an `event_rule` consumer fires, it writes the `event` row and the `alarm` transition in one
  transaction (the alarm transition is serialized per `(event_rule, owner)`); the API writes config,
  acks, and settings the same way. There is no row-lock single-fire worklist and no
  `LISTEN`/`NOTIFY` fan-out: the change is committed once, and the **CDC publisher** carries it
  outward.

The CDC publisher is **leader-elected** (exactly one active, fail over on death) via a NATS KV
CAS lock, the same singleton pattern the clock uses ([time](/architecture/time/)). It reads the WAL
by logical decoding and publishes each committed change to JetStream, where `action_rule`,
reconcile, and projection consumers react. The replication **slot** and **publication** it reads are
**ensured in the idempotent boot phase** (the same phase that upserts ship-with reference data),
**not** a run-once migration: boot creates them if absent and leaves them untouched if present, so a
fresh database and an existing one converge to the same state. Delivery is at-least-once with an
idempotency key per change, so a consumer that sees a change twice is a no-op.

:::

## Ground-truth records

The immutable, append-only records, each named for what it is. They are the lineage targets and what
a backtest reads; none is derived. The detailed columns of `audit_log` live on
[audit](/architecture/audit/), `session_log` on [nodes](/architecture/nodes/#sessions); the rest is a
compact list here because storage is their natural architectural home:

- **`log_line`** (a component's or node's own words, the untyped raw ingest lane, not a sample,
  [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events));
- **`audit_log`** (operator actions: actor, verb, resource, `old -> new`; the lineage target for
  operator writes; secret decrypts always recorded, [audit](/architecture/audit/)).

:::design[Target design, tracked in #430]
- **`session_log`** (connection-lifecycle transitions, node-reported; the connection log,
  [nodes](/architecture/nodes/#sessions));
- **`internal_log`** (platform self-narration: startup / reconcile / migration / node-reg /
  config-sync, [workers](/architecture/workers/));
- the **`collection_log`** / **`node_log`** companions (the cheap per-run execution record
  and the node's operational narration).
:::

There is **no separate rule-execution table**: a derived row *is* the evidence of its rule's run,
carrying its lineage on the row (below).

## The lineage CHECK (the pattern)

Lineage lives on the derived row, no separate execution table. This is the **pattern** every derived
row follows: **calculated** requires `source_rule` (+ version), the rule that produced the row;
**intended** requires the command `event_id` and no `source_rule`; **observed** requires only that
`event_id` is null (it may carry a `source_rule`, but the CHECK does not demand one); **declared**
requires neither pointer. The pointer per provenance is enforced so e.g. "intended with no command
event" is impossible at the storage layer. The real four-branch CHECK on the sample tables:

```sql
CHECK (
     (provenance = 'observed'   AND event_id IS NULL)
  OR (provenance = 'calculated' AND source_rule IS NOT NULL AND event_id IS NULL)
  OR (provenance = 'intended'   AND event_id IS NOT NULL AND source_rule IS NULL)
  OR (provenance = 'declared'   AND source_rule IS NULL AND event_id IS NULL)
)
```

Observed and calculated are distinguished by the **`provenance` column**, not a pointer-presence
trick (an edge function versus a calc_rule). The intended split is
the one the CHECK enforces. This is one of three layers: the CHECK enforces *which pointers are populated*, foreign keys enforce
*the ids are real*, and the app enforces *the value type matches the key's kind*.

The **trace columns live beside the lineage pointers, but not on the sample tables**: `event` carries
`correlation_id` and `source_event_id` (the causation parent, plus
`source_log_line_id` and `derived_by_rule_id`), and `log_line` carries `correlation_id`; `metric` and
`state` carry no trace columns today. These are orthogonal to the lineage CHECK.

:::design[Target design, tracked in #430]
The designed carriage: causation rides **NATS message headers** across the command -> device ->
observed-sample round trip and lands on the sample row, so the cycle guard walks a real id
([samples](/architecture/properties/), [alarms and actions](/architecture/alarms-actions/)).
:::

## Current value and projections: views by default

`alarm` and `action` are **stateful entities** that hold their own current state in a real table
(not event-sourced). Everything else that is "current state" is a **read model**, and the default is
a **plain SQL view** (always-correct, never stale, zero maintenance). A worker-maintained table is a
**measured optimization**, earned only when a read profile shows a view too slow. **The schema holds
zero SQL views today**; the shipped current-value read is the `property` cache table (the status note
above).

::::design[Target design, tracked in #430]

| Read model | Of | Shape | Notes |
|---|---|---|---|
| `current_value` | latest sample per (owner, key, **instance**, **provenance**), fused across sources per the key's `fusion_policy` | **view** | the dashboard read; per-provenance so observed and intended are both visible (the divergence model needs both), per-instance so siblings of one key stay distinct, fusion applied on read. The one table candidate if a profile earns it, metric kind only |
| `session` | `session_log` | **view** | low-volume; node, interface, status, opened_at, last_activity_at, command/error counts |

**When the view stops scaling.** A latest-per-key view's cost scales with the number of **distinct
keys** (a loose index scan), not total rows. Point and scoped reads ("current value of X on Y") are
a covering-index probe, fast at any size. A full-fleet "every current value" is O(distinct keys):
comfortable to hundreds of thousands, painful past a few million. A naive `DISTINCT ON` scans the
whole log and dies on the firehose; never that plan.

So only `current_value` for the **metric** firehose is even a table candidate, and only when
frequent full-fleet reads meet low-millions-plus distinct keys. The sparse kinds (`state` / `log`)
stay views indefinitely. A worker-maintained table costs **one upsert per sample write** (write
amplification, hot-key contention) and reintroduces a staleness window; that cost must be earned by
a read profile, not assumed. **Never a materialized view**: a PG MV is stale between refreshes and
has no incremental refresh, so a refresh is a full firehose recompute. The choice is plain view
(default) versus inline table (profiled).

:::caution[Open question]
If `current_value` is ever materialized, is it one wide table or a table per kind, keyed per (owner,
key, instance, provenance)?
:::

::::

::::design[Target design: partitioning is tracked in #420, retention in #417, and the `raw_sample` buffer in #430]

## Partitioning and retention

- **Append-only tables are range-partitioned by `ts`** (native declarative partitioning;
  `pg_partman` where the provider permits, else a documented manual roll). The firehose
  (`metric`) is the partitioning-critical one.
- **Retention is per table**, set by policy, not one blanket TTL: `metric` short,
  `state` longer, `audit_log` longest (compliance), `internal_log`
  short, and `log_line` keyed on its own axes (`severity` / `facility` are indexed columns precisely so
  retention and routing can discriminate: a debug line ages out long before a warning). On-row lineage
  ages out with its sample, and a `log_line` aged out from under an event it was derived from leaves
  `event.source_log_line_id` null rather than deleting the event (`on delete set null`). The per-table defaults are **cascade-resolved**
  ([cascade](/architecture/cascade/)) with an install-wide `platform` binding, so a class or entity can
  hold longer or shorter without changing the whole install.
- **The `raw_sample` buffer** (the opt-in raw-retention policy, [collection](/architecture/collection/))
  is range-partitioned by `ts` and cold-tierable like the metric partitions, on a short retention. It
  is bounded, sampled, and short-lived; it is not a telemetry table.
- **Views are not partitioned** (bounded by fleet size, not time) and are computed from the
  underlying tables, never the source of truth.

:::caution[Open question]
The index strategy per sample table beyond the obvious (BRIN on metric `ts`, GIN on log body),
tuned against real volume.
:::

:::caution[Open question]
The append-only id type under partitioning: bigint identity versus uuid v7.
:::

::::

## The Storage Gateway and tiering

The **Storage Gateway is the only door to the database** (no direct access, no
PostgREST); it is also where IAM scope is injected, **per action**: every query carries
`visible_set(P, action)` for the specific action it performs, so a read filters by read-scope and an
`:ack` write filters by ack-scope. A write whose action-scoped predicate matches **0 rows** is surfaced to
the handler as a 403 or 404, never a silent success, matching the up-front `canDo` decision
([identity and access](/architecture/identity-access/)). Isolation is per-database (one database per
tenant, paired one-to-one with one NATS account, [samples](/architecture/properties/)), so there
is no tenant context to set. Every read and write lands here. Scope arrives as an **explicit per-call
`scope.Set` argument**: the request path passes the caller's action-scoped set, and seed and system
callers pass an all-scope set. The named three-mode contract (scoped / node / system) that identity
and access describes is the Design formalization of that convention, not a built mode switch. The CDC
publisher reads committed changes by **logical decoding of the WAL**, a
replication-protocol stream beneath the table surface; that is how it learns of a change without
re-querying, not a second application path around the Gateway. Because every
application read and write goes through the Gateway, the physical backend is swappable beneath it:

- **default**: Postgres for everything (samples, ground-truth records, views, registries). Postgres
  is **BYO today**.
- **blobs**: opaque bytes (a firmware image, a config dump, a capture, and later a large `log_line`
  body or a `collection.failed` raw payload) live in the content-addressed [blob store](/architecture/files/),
  a `blob.Store` seam behind the same gateway. The default **pgblobs** backend holds bytes inline in
  Postgres, and a row references a blob by its `sha256`, never inline bytes.

:::design[Embedded Postgres, tracked in #19]
Embedded Postgres in the single binary (the same code path either way) replaces BYO for the
all-in-one run mode; the data lane's persistence consumer and the record lane's CDC publisher (#430)
target this one backend either way.
:::

::::design[The cold tier, tracked in #434]
**tiering**: the firehose does not stay in hot Postgres forever. Aged `metric` / `log_line`
partitions tier out to a **columnar or object store** (Parquet on S3-compatible, or an embedded
columnar engine) behind the same gateway, so historical queries fan across hot and cold with no model
change. The cold tier is partitioned by `ts`.

:::caution[Open question]
Which cold engine backs the tier, what triggers tier-out (age versus a partition-detach hook), how
queries federate across hot and cold, and whether projections ever tier.
:::
::::

:::design[Alternate blob backends, tracked in #248]
An S3-compatible or disk blob backend swaps in beneath the same `blob.Store` seam with no model
change, since rows reference blobs by `sha256`.
:::

:::design[Target design, tracked in #434]

## Query construction: typed, parameterized, generated

The gateway builds every query with **[jet](https://github.com/go-jet/jet)**, a type-safe SQL builder
whose column and table types are **generated from the dbmate-managed schema** (dbmate stays the single
schema authority; jet regenerates after `migrate`). The shape is dynamic (the per-action scope predicate,
the [filter expression](/architecture/expressions/), order, pagination compose at runtime) but the safety
is **structural, not by discipline**:

- **Values are always bound parameters**, never interpolated into SQL text.
- **Identifiers (columns, tables) are typed constants** from the generated schema, so a wrong or
  attacker-supplied column name is a **compile error**, never a string. The filter language's field names
  resolve against those same generated columns before they become a predicate.
- **Operators are a closed set.**

A wrong column or type fails the build, so the compiler and tests catch a bad query before runtime, which
is what keeps the gateway safe to evolve and safe for an AI to edit. Because all dynamic construction
lives in this one module, the injection-safe discipline is a single reviewable chokepoint. The one
carve-out is the high-volume sample insert (the persistence consumer), which may use `pgx` `COPY` for
throughput, still inside the gateway. It runs in all-visibility **system mode**, not per-row scoped: its
safety rests on the typed column targets plus the upstream **admission consumer** having already confined
owners ([identity and access](/architecture/identity-access/)), not on a per-write scope predicate.

:::
