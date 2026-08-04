---
title: Storage
description: "How storage works: the Storage Gateway, views by default, per-database isolation, append-only partitioning and tiering, and the on-row lineage pattern."
sidebar:
  badge:
    text: Partial
    variant: note
---

This page describes **how storage works**: the patterns every other leaf's entities land on (scope, audit, retention, lineage), not a per-table column dump.

:::note[Partial]
Built today: the Storage Gateway as the only door to the database, dbmate migrations (run-once,
embedded, idempotent), the per-action scope predicate and the in-transaction `audit_log` write, and
the shared scoped-tree and scoped-CRUD primitives. Scope arrives at the gateway as an explicit
per-call `scope.Set` argument; seed and system callers pass an all-scope set. Today `metric` /
`state` carry no `series` column and no uniqueness key, so redelivery can duplicate
([#430](https://github.com/hyperscaleav/omniglass/issues/430) stage 3). The `property` current-value cache is built, a table
upserted from the sink, not the metric-view once sketched here
([properties](/architecture/properties/#the-current-value-cache)).
See [implementation status](/architecture/status/).
:::

Postgres is the **relational system of record** (entities, events, alarms, actions, audit, config, settings): the record/state/intent lane. It is **never a message bus**: the live signal travels on NATS JetStream, Postgres is the durable record. Operator mutations and the record/state/intent lane (config, ack/snooze, settings, manual commands) write synchronously through the Storage Gateway.

:::design[Target design, tracked in #430]
**The sample tables are an async SINK**: the NATS **persistence consumer** batch-writes samples off
the data lane into `metric` / `state`, idempotent on `(series, ts)`
([#430](https://github.com/hyperscaleav/omniglass/issues/430) stage 3), so a redelivery lands the
same row and the firehose never blocks on the database. Raw log lines ride the same path into `log_line`, keyed on nothing (an
untyped arrival has no series). Every other row (the `event` and `alarm` rows an `event_rule`
consumer commits in one transaction, and every operator mutation) is born in a Postgres transaction
and fanned outward by the **CDC publisher**
([messaging](/architecture/messaging/#two-lanes-one-bus)).
:::

Column schemas live with each owning feature: [samples](/architecture/properties/#the-sample-tables), [events](/architecture/events/#storage), [alarms and actions](/architecture/alarms-actions/#storage) (`alarm` / `action`), [commands](/architecture/commands/), [config and credentials](/architecture/variables/#storage), [core entities](/architecture/core-entities/) and [templates](/architecture/templates/), [collection](/architecture/collection/#storage), [calculations](/architecture/calculations/#storage), [files](/architecture/files/), [time](/architecture/time/#storage), and [identity and access](/architecture/identity-access/#storage).

## Conventions

- **Identity is three columns.** **`id`** is a uuid: immutable, the primary key, and what every
  foreign key stores. **`name`** is the renameable identifier an operator types and an address
  carries (the `rm215a` in `boi.17c.rm215a`). **`display_name`** is an optional friendly string a
  human reads ("HQ Boardroom DSP"), and a surface that has none falls back to the name rather than
  re-casing it. A rename moves `name` and nothing else, which is why references store the id and why
  `audit_log.resource_id` does too. `storage.ValidateName` is the one validator, picking between the
  entity rule (one segment) and the keyspace rule (dot-joined segments) from the table's declared
  identity shape, so a call site cannot choose the wrong rule or forget to choose
  ([ADR-0076](/architecture/decisions/), [core entities](/architecture/core-entities/)).
- **No `tenant_id`.** Isolation is per-database; no tenant column anywhere. The registries and catalogs (`property_type`, `event_type`, `interface_type`, `location_type`, `secret_type`, `vendor`, `driver`, `capability`, `product`, `standard`) carry an **`official` boolean** (the per-registry template / org / official `scope` ladder is future design, [key scope](/architecture/properties/#key-scope-template-org-official)): `official: true` rows are the ship-with canonical set, `official: false` operator- or org-authored. The boolean is **authority, not provenance**: a `standard` and a `location_type` ship `official: false`, installed **only if absent** (example content an estate owns); the canonical catalogs ship `official: true` through an authoritative `ON CONFLICT DO UPDATE`, so a release can correct the shared vocabulary ([the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)).
- **Three storage shapes.** **Ground-truth records**: append-only, immutable, named for what they are ([below](#ground-truth-records)). There is **no `telemetry` table**: samples are published to the JetStream data lane (raw appears only on a `collection.failed` event or a dev raw-mode tap, [samples](/architecture/properties/)), and a schedule fire is an `event` with `origin=scheduled`. **Samples** (`metric` / `state`) are the typed firehose, `log_line` the untyped raw arrival beside them ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events): no property name, no registry gate). **Stateful entities and projections** (`alarm`, `action`, current-value) hold state directly or are rebuildable read models, **views by default**. The model is **not event-sourced**.
- **Provenance and lineage on every sample**: `provenance` (observed / calculated / intended / declared), `source` (which sensor or path, for observed), and a lineage pointer enforced per provenance by a CHECK ([the lineage CHECK](#the-lineage-check-the-pattern)). `declared` exists as a sample provenance in the schema ([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)), though the model going forward keeps declared config in [config](/architecture/variables/).
- **Ownership is the exclusive-arc**, though not one uniform arc: the sample tables and `event` carry `owner_kind` (`component` / `system` / `location` / `node`) plus the matching typed FK and a CHECK (no platform or global arm on a sample); `variable`'s arc is `platform` / `component` / `system` / `location` (no node arm; `platform` sets all three FKs null); `alarm` carries **no arc**, a single NOT NULL `component_id`, component-local by design today. Full pattern: [core entities](/architecture/core-entities/#ownership-the-exclusive-arc).
- **A write struct takes the `Write` suffix; the bare noun is the row**: `MetricSampleWrite` in, `MetricSample` back, likewise `StateSampleWrite`, `EventWrite`, `LogLineWrite`. A carrier is named for what it carries: hence the wire message is a `TelemetryBatch` ([ADR-0072](/architecture/decisions/#adr-0072-an-envelope-is-not-named-after-its-passengers-and-an-insert-struct-takes-the-write-suffix)).
- **Keys**: samples and events use a surrogate id plus `ts`; `property_type` is name-unique with the **`official` boolean** deciding authority; structural entities are name-keyed; a `task` is **content-addressed** (`sha256` over `(interface_id, mode, spec)`); a `node` by its `principal_id`. Every foreign key stores the target's primary key, so a rename is free ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)).

## How the records relate

The relationships, not the columns (those live on each owning leaf, linked above).

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

The structural and template entities relate as shown on [core entities](/architecture/core-entities/) and [templates](/architecture/templates/); the collection entities (`interface_type` / `interface` / `task`) on [collection](/architecture/collection/#storage).

## Ground-truth records

The immutable, append-only records: the lineage targets and what a backtest reads, none derived. Columns for `audit_log` live on [audit](/architecture/audit/), `session_log` on [nodes](/architecture/nodes/#sessions):

- **`log_line`** (a component's or node's own words, the untyped raw ingest lane, not a sample, [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events));
- **`audit_log`** (operator actions: actor, verb, resource, `old -> new`; the lineage target for operator writes; secret decrypts always recorded, [audit](/architecture/audit/)).

:::design[Target design, tracked in #430]
- **`session_log`** (connection-lifecycle transitions, node-reported, [nodes](/architecture/nodes/#sessions));
- **`internal_log`** (platform self-narration: startup / reconcile / migration / node-reg / config-sync, [workers](/architecture/workers/));
- the **`collection_log`** / **`node_log`** companions (the cheap per-run execution record and the node's operational narration).
:::

## The lineage CHECK (the pattern)

Lineage lives on the derived row, no separate execution table: a derived row *is* the evidence of its rule's run. The pointer per provenance is enforced, making "intended with no command event" impossible at the storage layer. The real four-branch CHECK on the sample tables:

```sql
CHECK (
     (provenance = 'observed'   AND event_id IS NULL)
  OR (provenance = 'calculated' AND source_rule IS NOT NULL AND event_id IS NULL)
  OR (provenance = 'intended'   AND event_id IS NOT NULL AND source_rule IS NULL)
  OR (provenance = 'declared'   AND source_rule IS NULL AND event_id IS NULL)
)
```

Observed and calculated are distinguished by the **`provenance` column**, not a pointer-presence trick. Three layers: the CHECK enforces *which pointers are populated*, foreign keys enforce *the ids are real*, the app enforces *the value type matches the key's kind*.

The **trace columns live beside the lineage pointers, but not on the sample tables**: `event` carries `correlation_id` and `source_event_id` (plus `source_log_line_id` and `derived_by_rule_id`), `log_line` carries `correlation_id`; `metric` and `state` carry none today. Orthogonal to the lineage CHECK.

:::design[Target design, tracked in #430]
The designed carriage: causation rides **NATS message headers** across the command -> device ->
observed-sample round trip and lands on the sample row, so the cycle guard walks a real id
([samples](/architecture/properties/), [alarms and actions](/architecture/alarms-actions/)).
:::

## Current value and projections: views by default

`alarm` and `action` are **stateful entities** holding current state in a real table (not event-sourced). Everything else that is "current state" is a **read model**, default a **plain SQL view** (always-correct, never stale, zero maintenance); a worker-maintained table is a **measured optimization**, earned only when a read profile shows a view too slow. **The schema holds zero SQL views today** (the shipped current-value read is the `property` cache table, per the status note).

::::design[Target design, tracked in #430]

| Read model | Of | Shape | Notes |
|---|---|---|---|
| `current_value` | latest sample per (owner, key, **instance**, **provenance**), fused across sources per the key's `fusion_policy` | **view** | the dashboard read; per-provenance so observed and intended are both visible (the divergence model needs both), per-instance so siblings of one key stay distinct, fusion applied on read. The one table candidate if a profile earns it, metric kind only |
| `session` | `session_log` | **view** | low-volume; node, interface, status, opened_at, last_activity_at, command/error counts |

**When the view stops scaling.** A latest-per-key view's cost scales with the number of **distinct
keys** (a loose index scan), not total rows: point and scoped reads are a covering-index probe, fast
at any size, while a full-fleet "every current value" is O(distinct keys), comfortable to hundreds of
thousands, painful past a few million (a naive `DISTINCT ON` scans the whole log; never that plan).
So only `current_value` for the **metric** firehose is even a table candidate, and only when
frequent full-fleet reads meet low-millions-plus distinct keys; the sparse kinds stay views
indefinitely. A worker-maintained table costs one upsert per sample write (write amplification,
hot-key contention) and reintroduces a staleness window: a cost earned by a read profile. **Never a
materialized view**: a PG MV is stale between refreshes with no incremental refresh. The choice is
plain view (default) versus inline table (profiled).

:::caution[Open question]
If `current_value` is ever materialized, is it one wide table or a table per kind, keyed per (owner,
key, instance, provenance)?
:::

::::

::::design[Target design: partitioning is tracked in #420, retention in #417, and the `raw_sample` buffer in #430]

## Partitioning and retention

- **Append-only tables are range-partitioned by `ts`** (native declarative partitioning; `pg_partman` where the provider permits, else a documented manual roll). The firehose (`metric`) is the partitioning-critical one.
- **Retention is per table**, set by policy: `metric` short, `state` longer, `audit_log` longest (compliance), `internal_log` short, and `log_line` keyed on its own axes (`severity` / `facility` are indexed precisely so retention and routing can discriminate). On-row lineage ages out with its sample, and a `log_line` aged out from under an event leaves `event.source_log_line_id` null (`on delete set null`), never deleting the event. Per-table defaults are **cascade-resolved** ([cascade](/architecture/cascade/)) with an install-wide `platform` binding.
- **The `raw_sample` buffer** ([collection](/architecture/collection/)) is range-partitioned by `ts` and cold-tierable like the metric partitions, on a short retention: bounded, sampled, short-lived, not a telemetry table.
- **Views are not partitioned** (bounded by fleet size, not time), computed from the underlying tables, never the source of truth.

:::caution[Open question]
The index strategy per sample table beyond the obvious (BRIN on metric `ts`, GIN on log body),
tuned against real volume.
:::

:::caution[Open question]
The append-only id type under partitioning: bigint identity versus uuid v7.
:::

::::

## The Storage Gateway and tiering

The **Storage Gateway is the only door to the database** (no direct access, no PostgREST), and it injects IAM scope **per action**: every query carries `visible_set(P, action)` for the specific action it performs, so a read filters by read-scope and an `:ack` write by ack-scope. A write whose action-scoped predicate matches **0 rows** surfaces as a 403 or 404, never a silent success, matching the up-front `canDo` decision ([identity and access](/architecture/identity-access/)). Per-database isolation (one database per tenant, paired one-to-one with one NATS account) means no tenant context to set. Scope arrives as an **explicit per-call `scope.Set` argument**; the named three-mode contract (scoped / node / system) is the Design formalization of that convention, not a built mode switch. The [CDC publisher](/architecture/messaging/#two-lanes-one-bus) reads committed changes from the WAL, a replication-protocol stream beneath the table surface, not a second path around the Gateway. Every read and write goes through the Gateway, so the physical backend swaps beneath it:

- **default**: Postgres for everything (samples, ground-truth records, views, registries). Postgres is **BYO today**.
- **blobs**: opaque bytes (a firmware image, a config dump, a capture, later a large `log_line` body or a `collection.failed` raw payload) live in the content-addressed [blob store](/architecture/files/), a `blob.Store` seam behind the same gateway. The default **pgblobs** backend holds bytes inline in Postgres; a row references a blob by its `sha256`, never inline bytes.

:::design[Embedded Postgres, tracked in #19]
Embedded Postgres in the single binary replaces BYO for the all-in-one run mode; the data lane's
persistence consumer and the record lane's CDC publisher (#430) target this one backend through the
same code path either way.
:::

::::design[The cold tier, tracked in #529]
**tiering**: aged `metric` / `log_line` partitions tier out to a **columnar or object store**
(Parquet on S3-compatible, or an embedded columnar engine) behind the same gateway, so historical
queries fan across hot and cold with no model change. The cold tier is partitioned by `ts`.

:::caution[Open question]
Which cold engine backs the tier, what triggers tier-out (age versus a partition-detach hook), how
queries federate across hot and cold, and whether projections ever tier.
:::
::::

:::design[Alternate blob backends, tracked in #248]
An S3-compatible or disk blob backend swaps in beneath the same `blob.Store` seam with no model
change, since rows reference blobs by `sha256`.
:::

:::design[Typed, generated query construction, tracked in #529]

## Query construction: typed, parameterized, generated

The gateway builds every query with **[jet](https://github.com/go-jet/jet)**, a type-safe SQL builder whose column and table types are **generated from the dbmate-managed schema** (dbmate stays the single schema authority; jet regenerates after `migrate`). The shape is dynamic (the per-action scope predicate, the [filter expression](/architecture/expressions/), order, pagination compose at runtime) but the safety is **structural, not by discipline**: values are always bound parameters, never interpolated; identifiers are typed constants from the generated schema, so a wrong or attacker-supplied column name is a **compile error** (the filter language's field names resolve against those same generated columns); operators are a closed set. All dynamic construction lives in this one module, a single reviewable chokepoint. The one carve-out, the high-volume sample insert (the persistence consumer), may use `pgx` `COPY` for throughput, still inside the gateway: it runs in all-visibility **system mode**, its safety resting on the typed column targets plus the upstream **admission consumer** having already confined owners ([identity and access](/architecture/identity-access/)), not on a per-write scope predicate.

:::
