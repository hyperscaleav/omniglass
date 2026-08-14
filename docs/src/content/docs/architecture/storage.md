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
`property` carry no `series` column and no uniqueness key, so redelivery can duplicate
([#430](https://github.com/hyperscaleav/omniglass/issues/430) stage 3). Current values are derived
from the series (latest row per series); the maintained latest-value cache retired
([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)),
and the provenance-aware retention floor is built as `PruneSamples`
([ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)).
See [implementation status](/architecture/status/).
:::

Postgres is the **relational system of record** (entities, events, alarms, actions, audit, config, settings): the record/state/intent lane. It is **never a message bus**: the live signal travels on NATS JetStream, Postgres is the durable record. Operator mutations and the record/state/intent lane (config, ack/snooze, settings, manual commands) write synchronously through the Storage Gateway.

:::design[Target design, tracked in #430]
**The sample tables are an async SINK**: the NATS **persistence consumer** batch-writes samples off
the data lane into `metric` / `property`, idempotent on `(series, ts)`
([#430](https://github.com/hyperscaleav/omniglass/issues/430) stage 3), so a redelivery lands the
same row and the firehose never blocks on the database. Raw log lines ride the same path into `log_line` and `node_log`, keyed on nothing (an
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
  `audit_log.resource_id` does too. `storage.ValidateName` is the one validator, applying the one
  kebab name rule to every table whose declared identity shape bears a name, so a call site cannot
  skip validation or invent a second rule
  ([ADR-0076](/architecture/decisions/), [core entities](/architecture/core-entities/)).
- **No `tenant_id`.** Isolation is per-database; no tenant column anywhere. The registries and catalogs (`metric_type`, `property_type`, `event_type`, `interface_type`, `location_type`, `secret_type`, `vendor`, `driver`, `component_type`, `system_type`, `product`, `standard`) carry an **`official` boolean** (the per-registry template / org / official `scope` ladder is future design, [property scope](/architecture/properties/#key-scope-template-org-official)): `official: true` rows are the ship-with canonical set, `official: false` operator- or org-authored. The boolean is **authority, not provenance**: a `standard` ships `official: false`, installed **only if absent** (example content an estate owns); the canonical catalogs, `location_type` among them since [ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask), ship `official: true` through an authoritative `ON CONFLICT DO UPDATE`, so a release can correct or withdraw the shared vocabulary ([the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)). An `official: true` row is never written by an operator, but it is no longer a dead end: an edit **forks** it into `registry_shadow`, a registry-agnostic table keyed `(registry, row_id)` on the shipped row's own uuid, and reads resolve the shadow over the official row ([ADR-0095](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)). One uuid and one name per logical row either way, so no foreign key, walk, or URL learns about the fork; restore is deleting the shadow. `component_type` is the first adopter and `location_type` the second.
- **Three storage shapes.** **Ground-truth records**: append-only, immutable, named for what they are ([below](#ground-truth-records)). There is **no `telemetry` table**: samples are published to the JetStream data lane (raw appears only on a `collection.failed` event or a dev raw-mode tap, [samples](/architecture/properties/)), and a schedule fire is an `event` with `origin=scheduled`. **Samples** (`metric` / `property`) are the typed firehose, `log_line` and `node_log` the untyped raw arrival beside them ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events): no registered name, no catalog gate). **Stateful entities and projections** (`alarm`, `action`) hold state directly; everything "current" is a rebuildable read, **views by default**. The model is **not event-sourced**.
- **Provenance and lineage on every sample**: `provenance` (observed / calculated / intended / declared), `source` (which sensor or path, for observed), and a lineage pointer enforced per provenance by a CHECK ([the lineage CHECK](#the-lineage-check-the-pattern)). A `declared` row is an operator's assertion recorded in the series itself ([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)); the metric lane admits the first three provenances today (no declared-metric writer exists, and its CHECK refuses the row).
- **Ownership is the exclusive-arc**, though not one uniform arc: the sample tables, `event`, and `command` carry `owner_kind` (`component` / `system` / `location` / `node`) plus the matching typed FK and a CHECK (no platform or global arm on a sample); `log_line` is **component-only** (a single NOT NULL `component_id`; a node's self-logs live in `node_log`); `variable`'s arc is `platform` / `component` / `system` / `location` (no node arm; `platform` sets all three FKs null); `alarm` carries **no arc**, a single NOT NULL `component_id`, component-local by design today. Full pattern: [core entities](/architecture/core-entities/#ownership-the-exclusive-arc).
- **A write struct takes the `Write` suffix; the bare noun is the row**: `MetricSampleWrite` in, `MetricSample` back, likewise `PropertySampleWrite`, `EventWrite`, `LogLineWrite`. A carrier is named for what it carries: hence the wire message is a `TelemetryBatch` ([ADR-0072](/architecture/decisions/#adr-0072-an-envelope-is-not-named-after-its-passengers-and-an-insert-struct-takes-the-write-suffix)).
- **Keys**: samples and events use a surrogate id plus `ts`; each catalog (`metric_type`, `property_type`, `event_type`, `command_type`) is name-unique with the **`official` boolean** deciding authority, and the three ingest catalogs refuse a name a sibling holds; structural entities carry a unique, renameable `name` over a uuid primary key; a `task` is **content-addressed** (`sha256` over `(interface_id, mode, spec)`); a `node` by its `principal_id`. Every foreign key stores the target's primary key, so a rename is free ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)).
- **A `location`, `system`, or `component` name is unique within its placement, not across the estate**
  ([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)):
  each table trades its old global `UNIQUE (name)` constraint for a set of partial unique indexes, one
  per placement bucket, plus a plain btree on the bare `name` column for the ambiguity scan every
  bare-name resolve runs. `component` and `system` both carry three buckets (parent, location, orphan:
  `component_parent_name_key` / `component_location_name_key` / `component_orphan_name_key` and the
  matching triple on `system`), since both carry their own `parent_id` and `location_id`. `location`
  carries only two (`location_parent_name_key` / `location_root_name_key`): it has no `location_id`
  column of its own, so its two buckets are parented and root
  (`db/migrations/20260808090000_names_scope_to_placement.sql`). A **dotted address**
  (`boi.17c.415a.$comp.display-1`) resolves structurally against these same indexes, one deterministic
  hop per segment, before any scope or ambiguity check runs; see [core entities](/architecture/core-entities/#an-address-a-uuid-or-a-dotted-path)
  for the grammar and [identity and access](/architecture/identity-access/#a-reference-resolves-within-a-scope)
  for how scope and ambiguity are then decided against the resolved candidate set.

## Migrations: three buckets, kept separate

A schema change is authored with **dbmate**: pure-DDL migrations under `db/migrations/`, embedded into
the binary and applied by the `migrate` run mode. Two rules hold everywhere: a migration **runs exactly
once** (dbmate keys on the timestamp version, not the contents, so it is never edited after it ships,
only followed by a new one), and DDL is **idempotent** (`IF NOT EXISTS`, a guarded `DO` block for a
Postgres statement with no `IF NOT EXISTS` form of its own, e.g. a column rename).

A change that both reshapes the schema and needs default rows for the shape to be usable never mixes
the two in one migration. Three buckets, never conflated:

- **Schema migrations** (`db/migrations/*.sql`, dbmate): pure DDL. No seed rows: a schema dump or a
  future squash silently drops any row a migration inserted, so a migration that seeds data is a landmine
  for whoever collapses the chain later.
- **Boot seed phase** (idempotent upsert on every server start, `internal/seed/*.yaml`): ship-with
  reference data, authoritative via `ON CONFLICT DO UPDATE` for a canonical catalog (a release can
  correct it, and can **withdraw** a value it once shipped, which is the half insert-if-absent cannot
  do at all); operator rows are never touched. `location_type` seeds this way since
  [ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask),
  which is what an authoritative seed COSTS: the rows have to be platform-owned, so an operator's
  version of one lives in `registry_shadow` and resolves over it. **Narrow carve-out:** a seeded table can additionally
  reconcile its child rows to the declared set every boot, deleting one that dropped out (refusing
  instead if something still points at it) rather than leaving it in place, but only where BOTH hold:
  the table has **no operator write path** (nothing but the seed ever writes a row there) and its rows
  carry a **packed positional ordering** where a leftover orphan does not sit inert, it collides with
  the position a renamed or reordered entry now wants. `choice_alternate` is the only table this
  applies to today
  ([ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability)).
  Absent both preconditions, an operator-writable or unordered table keeps the ordinary
  insert-if-absent rule above.

  **A row that is both shipped and operator-owned splits into two columns.** The global label rules
  (`label_rule`, one row per labelled entity kind) carry `default_template` and `template`: the seed
  writes only the first, authoritatively, and the second is the operator's, resolved over it. Neither
  single-column arrangement works, since an authoritative seed stomps the operator on the next
  restart and a seed-if-absent freezes the shipped default at the first boot. It is the same
  shipped-values-and-operator-values-live-apart shape the registry fork gives a registry row, at the
  scale where an overlay table would be more machinery than the three rows are worth
  ([ADR-0098](/architecture/decisions/#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)).
  **A derived column is maintained by the gateway and proved by a recompute-and-compare.** The
  generated label is the worked example: it is stored, so sort, filter and search stay in SQL, and the
  staleness that buys is paid for by an invariant rather than by a trigger (logic lives in Go, never
  in the database). What that invariant is stopped being an enumeration of write paths when a rule
  gained the ability to read facts on OTHER rows: it is now an estate-wide question the gateway
  answers, `PreviewLabelRecompute` returning nothing, so a write path nobody thought of fails it
  rather than a list nobody updated passing
  ([ADR-0100](/architecture/decisions/#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)).

- **One-time data backfills** (dbmate, data-only): transforming existing operator rows to match a new
  constraint, run once, and idempotent on a second run (a repeat changes nothing, proven by a test that
  executes the migration's up-SQL twice).

**Worked example: the product classification floor** (#614). Making `product.component_type_id` and
`component.product_id` both `NOT NULL` needed all three buckets, landed as three migrations in this
order, because reversing the order would either fail (a `NOT NULL` added before any row satisfies it)
or silently orphan existing operator components (a backfill run against a still-optional column has
nothing forcing it to run at all):

1. **Schema (nullable):** `20260807110000_product_component_type_and_icon.sql` adds
   `product.component_type_id` (nullable, FK to `component_type`, `on delete restrict`) and
   `product.icon` (nullable). Pure DDL, safe against any existing row.
2. **Boot seed, then backfill:** the boot seed ships the `component_type` tree
   (`internal/seed/component_types.yaml`) and three generic products
   (`internal/seed/products.yaml`) pointing at the matching generic types, so the chain a backfill
   needs already exists by the time it runs. `20260807113000_product_type_backfill.sql` is data-only
   and idempotent (`ON CONFLICT (name) DO NOTHING` for the generics it also inserts defensively, `WHERE
   component_type_id IS NULL` / `WHERE product_id IS NULL` guards on the updates): it folds
   `kind='vm'` to `'app'`, points every null `product.component_type_id` at the type matching the
   product's kind, and points every null `component.product_id` at `generic-device`.
3. **Schema (floor):** `20260807116000_product_type_floor.sql` sets both columns `NOT NULL` and
   narrows the `kind` check constraint to `device | app | service`. Pure DDL again, now safe because
   step 2 already closed every gap it depends on.

Running the chain against a fresh database is the same three steps in the same order: nullable column,
then the seed and backfill that make every row satisfy the coming constraint, then the constraint
itself. A fresh database and an upgraded one converge on the identical end state, which is the point of
keeping the buckets separate rather than reaching for a single migration with an inline `UPDATE`.

## How the records relate

The relationships, not the columns (those live on each owning leaf, linked above).

```d2
direction: right
classes: { node: { style.border-radius: 8 } }
metric: metric { class: node }
property: property { class: node }
event: event { class: node }
alarm: alarm { class: node }
action: action { class: node }
command: command { class: node }
metric -> metric: calc_rule
property -> event: event_rule
event -> alarm: fire opens · clear resolves
event -> action: action_rule
alarm -> action
command -> property: opens an intended value
command -> metric: opens an intended value
```

The structural and template entities relate as shown on [core entities](/architecture/core-entities/) and [templates](/architecture/templates/); the collection entities (`interface_type` / `interface` / `task`) on [collection](/architecture/collection/#storage).

## Ground-truth records

The immutable, append-only records: the lineage targets and what a backtest reads, none derived. Columns for `audit_log` live on [audit](/architecture/audit/), `session_log` on [nodes](/architecture/nodes/#sessions):

- **`log_line`** (a **component's** own words, the untyped raw ingest lane, not a sample, component-only, [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events));
- **`node_log`** (a **node's** self-logs, the same payload shape without the owner arc, split by origin, [ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus));
- **`audit_log`** (operator actions: actor, verb, resource, `old -> new`; the lineage target for operator writes; secret decrypts always recorded, [audit](/architecture/audit/)).

:::design[Target design, tracked in #430]
- **`session_log`** (connection-lifecycle transitions, node-reported, [nodes](/architecture/nodes/#sessions));
- **`internal_log`** (platform self-narration: startup / reconcile / migration / node-reg / config-sync, [workers](/architecture/workers/));
- the **`collection_log`** companion (the cheap per-run execution record).
:::

## The lineage CHECK (the pattern)

Lineage lives on the derived row, no separate execution table: a derived row *is* the evidence of its rule's run. The pointer per provenance is enforced, making "intended with no command" impossible at the storage layer. The real four-branch CHECK on `property` (the metric lane carries the same CHECK minus the declared arm, since no declared-metric writer exists yet):

```sql
CHECK (
     (provenance = 'observed'   AND event_id IS NULL AND command_id IS NULL)
  OR (provenance = 'calculated' AND source_rule IS NOT NULL AND event_id IS NULL AND command_id IS NULL)
  OR (provenance = 'intended'   AND command_id IS NOT NULL AND source_rule IS NULL)
  OR (provenance = 'declared'   AND source_rule IS NULL AND event_id IS NULL AND command_id IS NULL)
)
```

Observed and calculated are distinguished by the **`provenance` column**, not a pointer-presence trick. An intended value names its **command** (`command_id`), not the event derived from it; the caused event stays stamped but optional ([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)). Three layers: the CHECK enforces *which pointers are populated*, foreign keys enforce *the ids are real*, the app enforces *the value conforms to the catalog's `data_type` and validation*.

The **trace columns live beside the lineage pointers, but not on the sample tables**: `event` carries `correlation_id` and `source_event_id` (plus `source_log_line_id` and `derived_by_rule_id`), `log_line` and `node_log` carry `correlation_id`; `metric` and `property` carry none today. Orthogonal to the lineage CHECK.

:::design[Target design, tracked in #430]
The designed carriage: causation rides **NATS message headers** across the command -> device ->
observed-sample round trip and lands on the sample row, so the cycle guard walks a real id
([samples](/architecture/properties/), [alarms and actions](/architecture/alarms-actions/)).
:::

## Current value and projections: views by default

`alarm` and `action` are **stateful entities** holding current state in a real table (not event-sourced). Everything else that is "current state" is a **read model**, default a **plain SQL view or a per-series indexed read** (always-correct, never stale, zero maintenance); a worker-maintained table is a **measured optimization**, earned only when a read profile shows the derived read too slow. **The schema holds zero SQL views and zero maintained caches today**: the shipped current-value read is the latest series row, derived on read; the once-built latest-value cache retired with the fold ([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)).

### A series read carries an owner index for the arc it reads

"Derived on read" only stays cheap while the read is a per-series **indexed** read rather than a scan
of the series, so `property` carries one partial owner index per arc a read is built on:
`property_owner_idx` (`component_id, property_type_id, instance, ts desc`) for the component arc, and
`property_system_owner_idx` (`property_type_id, system_id, id desc`) for the system arc, which the two
health reads take, the bulk verdict read behind `GET /systems:health` and the location rollup a
recompute pays per location ([#725](https://github.com/hyperscaleav/omniglass/issues/725)). Each is
**partial on its arc column**, so the telemetry lane pays nothing for an index it never uses: a
component-owned insert does not touch the system index at all (measured: 150,000 of them cost the same
to the millisecond with it and without it), while the system arc's own writes, a health transition and
only on a transition, pay about 2.7us a row. Adding it took the bulk read over 1,500 systems from
51 ms to 10 ms, and the location rollup from 45 ms to 0.7 ms, at 1,521,600 property rows.

An index existing is not an index being reached, and the ways a read stops reaching one leave
`pg_indexes` reporting it present throughout, so both reads are held to reaching it by an
**access-path assertion** rather than a catalog test
([ADR-0094](/architecture/decisions/#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing)
as amended, and [test-driven](/contributing/test-driven/) for the instrument). The next arc that grows
a bulk read of the series adds its index the same way: measured first, guarded second.

::::design[Target design, tracked in #430]

| Read model | Of | Shape | Notes |
|---|---|---|---|
| `current_value` | latest sample per series (owner, type, **instance**, **provenance**), fused across sources per the type's `fusion_policy` | **view** | the dashboard read; per-provenance so observed and intended are both visible (the divergence model needs both), per-instance so siblings of one name stay distinct, fusion applied on read. The one table candidate if a profile earns it, metric lane only |
| `session` | `session_log` | **view** | low-volume; node, interface, status, opened_at, last_activity_at, command/error counts |

**When the view stops scaling.** A latest-per-series view's cost scales with the number of **distinct
series** (a loose index scan), not total rows: point and scoped reads are a covering-index probe, fast
at any size, while a full-fleet "every current value" is O(distinct series), comfortable to hundreds of
thousands, painful past a few million (a naive `DISTINCT ON` scans the whole log; never that plan).
So only `current_value` for the **metric** firehose is even a table candidate, and only when
frequent full-fleet reads meet low-millions-plus distinct series; the sparse lanes stay derived
reads indefinitely. A worker-maintained table costs one upsert per sample write (write amplification,
hot-row contention) and reintroduces a staleness window: a cost earned by a read profile. **Never a
materialized view**: a PG MV is stale between refreshes with no incremental refresh. The choice is
plain view (default) versus inline table (profiled).

:::caution[Open question]
If `current_value` is ever materialized, is it one wide table or a table per lane, keyed per (owner,
type, instance, provenance)?
:::

::::

## Retention: the provenance-aware floor

Retention as a feature is unbuilt, but its one non-negotiable rule shipped first, as the
**`PruneSamples`** gateway primitive ([ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)):
a prune deletes series rows older than a cutoff from both sample tables, **except** any
`declared` row (an operator's assertion is the whole truth however old, not a sample) and
**except** the latest row of every series (a prune must not erase a current value). No caller
wires it yet; any future retention feature calls this primitive rather than writing its own
`DELETE`, so the first purge cannot silently destroy a declared value or a current reading.

::::design[Target design: partitioning is tracked in #420, retention in #417, and the `raw_sample` buffer in #430]

## Partitioning and retention

- **Append-only tables are range-partitioned by `ts`** (native declarative partitioning; `pg_partman` where the provider permits, else a documented manual roll). The firehose (`metric`) is the partitioning-critical one.
- **Retention is per table**, set by policy: `metric` short, `property` longer, `audit_log` longest (compliance), `internal_log` short, and `log_line` / `node_log` keyed on their own axes (`severity` / `facility` are indexed precisely so retention and routing can discriminate). On-row lineage ages out with its sample, and a `log_line` aged out from under an event leaves `event.source_log_line_id` null (`on delete set null`), never deleting the event. Per-table defaults are **cascade-resolved** ([cascade](/architecture/cascade/)) with an install-wide `platform` binding. Every path obeys the `PruneSamples` floor above.
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

The gateway also accepts a **query tracer** (`storage.WithQueryTracer`), installed on the pool's
connection config so it observes every statement the gateway issues, whichever method issued it.
That whole-pool reach is the point: the read paths query the pool directly, so a wrapper around a
call argument cannot see them. It is where an OpenTelemetry pgx tracer attaches, and it is the seam
the test harness counts round trips through, which is how a LIST is held to a constant number of
queries rather than an N+1 ([counting round trips](/contributing/test-driven/#counting-round-trips-not-timing-them)).

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

**Logic lives in Go, and the schema carries exactly one deliberate exception.** That exception is
the **owner-invariant** guard: `assert_owner_grant_exists()` and the `DEFERRABLE INITIALLY DEFERRED`
constraint trigger `principal_grant_owner_guard`, which refuse to leave the estate with zero
`owner @ all` grants at `COMMIT` ([ADR-0006](/architecture/decisions/)). It is a trigger on purpose,
because the property it holds is true only at commit time and no application check can be: two
transactions each revoking the second-to-last owner grant both see one remaining.

Everything else composes at the gateway. The last stored function that was NOT that guard retired
with
[ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions):
what names a principal (a human's username, else a service account's name) is now declared once in
the gateway and rendered into every statement that needs it, so a caller picks a SHAPE and never a
column. A read over many rows LEFT JOINs the sources and folds them in Go; the two positions a join
cannot reach (an `UPDATE ... RETURNING`, and the audit insert that denormalizes the actor inside the
caller's transaction, where a Go fold would cost a second round trip on every operator write) render
the sources as correlated sub-selects instead. Which shape is not a taste: measured on a
500-member group roster, the sub-select shape projected AND sorted on costs 3011 shared buffer hits
where the join costs 18, because Postgres does not common up two identical scalar sub-selects. Every
shape of one policy drifts unless something compares them, so an invariant test drives every
principal kind through all of them and fails on the first disagreement, the same
recompute-and-compare shape a derived column is held to above.

The gateway builds every query with **[jet](https://github.com/go-jet/jet)**, a type-safe SQL builder whose column and table types are **generated from the dbmate-managed schema** (dbmate stays the single schema authority; jet regenerates after `migrate`). The shape is dynamic (the per-action scope predicate, the [filter expression](/architecture/expressions/), order, pagination compose at runtime) but the safety is **structural, not by discipline**: values are always bound parameters, never interpolated; identifiers are typed constants from the generated schema, so a wrong or attacker-supplied column name is a **compile error** (the filter language's field names resolve against those same generated columns); operators are a closed set. All dynamic construction lives in this one module, a single reviewable chokepoint. The one carve-out, the high-volume sample insert (the persistence consumer), may use `pgx` `COPY` for throughput, still inside the gateway: it runs in all-visibility **system mode**, its safety resting on the typed column targets plus the upstream **admission consumer** having already confined owners ([identity and access](/architecture/identity-access/)), not on a per-write scope predicate.

:::
