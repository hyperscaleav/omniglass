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
embedded, idempotent, refused by an older binary, and serialized across concurrent runs), the per-action scope predicate and the in-transaction `audit_log` write, the
writer-class check on every sample and event write, with legacy rows held to it
([below](#every-key-has-one-writer)), and the
shared scoped-tree and scoped-CRUD primitives. Scope arrives at the gateway as an explicit
per-call `scope.Set` argument; seed and system callers pass an all-scope set. An ingest batch
lands in one transaction under its receipt (`telemetry_receipt`), so a redelivery lands each row
once while `metric` / `property` carry no uniqueness key of their own
([ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).
The metric lane is narrow and partitioned by month: a sample row is `(id, series_id, ts, value)`,
what it is a sample of is one `metric_series` row, and a command's intended rows live apart in
`metric_declared` ([below](#the-narrow-metric-lane),
[ADR-1275](/architecture/decisions/#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month)). Current values are derived
from the series (latest row per series); the maintained latest-value cache retired
([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)),
and the server prunes `metric` and `property` samples to their retention windows above the
provenance-aware floor, and `log_line` and `node_log` by severity band
([ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series),
[ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks)).
See [implementation status](/architecture/status/).
:::

Postgres is the **relational system of record** (entities, events, alarms, actions, audit, config, settings): the record/state/intent lane. It is **never a message bus**: the live signal travels on NATS JetStream, Postgres is the durable record. Operator mutations and the record/state/intent lane (config, ack/snooze, settings, manual commands) write synchronously through the Storage Gateway.

Built today, the ingest consumer writes a batch's every sink (`metric`, `property`, `event`,
`log_line`, `node_log`) in **one transaction** whose first statement inserts the batch's
**receipt**, `telemetry_receipt (batch_id, landed_at)`, keyed by the subject the batch arrived on and
its `Nats-Msg-Id`: all or nothing, and a batch id that already landed writes nothing, so a
redelivery lands each row once, raw log lines included, though a log line has no key of its own.
Receipts are pruned past a day by the server
([ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).

:::design[Target design, tracked in #430]
**The sample tables are an async SINK**: the NATS **persistence consumer** batch-writes samples off
the data lane into `metric` / `property`, idempotent per batch as the built consumer is, so the
firehose never blocks on the database. Raw log lines ride the same path into `log_line` and
`node_log`. Every other row (the `event` and `alarm` rows an `event_rule`
consumer commits in one transaction, and every operator mutation) is born in a Postgres transaction
and fanned outward by the **CDC publisher**
([messaging](/architecture/messaging/#two-lanes-one-bus)).
:::

Column schemas live with each owning feature: [samples](/architecture/properties/#the-sample-tables), [events](/architecture/events/#storage), [alarms and actions](/architecture/alarms-actions/#storage) (`alarm` / `action`), [commands](/architecture/commands/), [config and credentials](/architecture/variables/#storage), [core entities](/architecture/core-entities/) and [templates](/architecture/templates/), [collection](/architecture/collection/#storage), [calculations](/architecture/calculations/#storage), [files](/architecture/files/), [time](/architecture/time/#storage), and [identity and access](/architecture/identity-access/#storage).

## Conventions

- **Identity is three columns.** **`id`** is a uuid: immutable, the primary key, and what every
  foreign key stores. **`name`** is the renameable identifier an operator types and an address
  carries (the `rm215a` in `boi.17c.rm215a`). **`label`** is an optional friendly string a
  human reads ("HQ Boardroom DSP"), and a surface that has none falls back to the name rather than
  re-casing it. A rename moves `name` and nothing else, which is why references store the id and why
  `audit_log.resource_id` does too. `storage.ValidateName` is the one validator, applying the one
  kebab name rule to every table whose declared identity shape bears a name, so a call site cannot
  skip validation or invent a second rule
  ([ADR-0076](/architecture/decisions/), [core entities](/architecture/core-entities/)).
- **No `tenant_id`.** Isolation is per-database; no tenant column anywhere. The registries and catalogs (`metric_type`, `property_type`, `event_type`, `command_type`, `component_type`, `system_type`, `location_type`, `secret_type`, `severity_level`, `notification_type`, `event_rule`, `action_rule`, `calc_rule`, `vendor`, `product`, `driver`, `standard`, `role`) carry an **`official` boolean** and a **`namespace`** ([ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)): `og` is the platform's and only the boot seed writes it, `local` is an operator's default, and an `og` row is always official (the per-registry org / official `scope` ladder is future design, [property scope](/architecture/properties/#key-scope-org-and-official)): `official: true` rows are the ship-with canonical set, `official: false` operator- or org-authored. The boolean is **authority, not provenance**: a `standard` ships `official: false`, installed **only if absent** (example content a fleet owns); the canonical catalogs, `location_type` among them since [ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask), ship `official: true` through an authoritative `ON CONFLICT DO UPDATE`, so a release can correct or withdraw the shared vocabulary ([the seed model](/architecture/core-entities/#the-seed-model-templates-versus-canonical-catalogs)). An `official: true` row is never written by an operator, but it is no longer a dead end: an edit **forks** it into `registry_shadow`, a registry-agnostic table keyed `(registry, row_id)` on the shipped row's own uuid, and reads resolve the shadow over the official row ([ADR-0095](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)). One uuid per logical row either way (a fork never adds a row; a name names one row per namespace, [ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)), so no foreign key, walk, or URL learns about the fork; restore is deleting the shadow. `component_type` is the first adopter and `location_type` the second.
- **Three storage shapes.** **Ground-truth records**: append-only, immutable, named for what they are ([below](#ground-truth-records)). There is **no `telemetry` table**: samples are published to the JetStream data lane (raw appears only on a `collection.failed` event or a dev raw-mode tap, [samples](/architecture/properties/)), and a schedule fire is an `event` with `origin=scheduled`. **Samples** (`metric` / `property`) are the typed firehose, `log_line` and `node_log` the untyped raw arrival beside them ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events): no registered name, no catalog gate). **Stateful entities and projections** (`alarm`, `action`) hold state directly; everything "current" is a rebuildable read, **views by default**. The model is **not event-sourced**.
- **Provenance and lineage on every sample**: `provenance` (observed / calculated / intended / declared), `source` (which sensor or path, for observed), and a lineage pointer enforced per provenance by a CHECK ([the lineage CHECK](#the-lineage-check-the-pattern)). A `declared` row is an operator's assertion recorded in the series itself ([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)); the metric lane admits the first three provenances today (no declared-metric writer exists, and its CHECK refuses the row).
- **Ownership is the exclusive-arc**, though not one uniform arc: the sample tables, `event`, and `command` carry `owner_kind` (`component` / `system` / `location` / `node`) plus the matching typed FK and a CHECK (no platform or global arm on a sample; a `location` owns no observed sample, only its declared properties and its calculated health, since nothing observes a place, [ADR-0422](/architecture/decisions/#adr-0422-a-system-owns-its-own-telemetry-and-a-location-owns-no-observed-sample)); `log_line` is **component-only** (a single NOT NULL `component_id`; a node's self-logs live in `node_log`); `variable`'s arc is `platform` / `component` / `system` / `location` (no node arm; `platform` sets all three FKs null); `alarm` carries **no arc**, a single NOT NULL `component_id`, component-local by design today. Full pattern: [core entities](/architecture/core-entities/#ownership-the-exclusive-arc).
- **A write struct takes the `Write` suffix; the bare noun is the row**: `MetricSampleWrite` in, `MetricSample` back, likewise `PropertySampleWrite`, `EventWrite`, `LogLineWrite`. A carrier is named for what it carries: hence the wire message is a `TelemetryBatch` ([ADR-0072](/architecture/decisions/#adr-0072-an-envelope-is-not-named-after-its-passengers-and-an-insert-struct-takes-the-write-suffix)).
- **Keys**: samples and events use a surrogate id plus `ts`; each catalog (`metric_type`, `property_type`, `event_type`, `command_type`) is name-unique with the **`official` boolean** deciding authority, and the three ingest catalogs refuse a name a sibling holds; structural entities carry a unique, renameable `name` over a uuid primary key; a `task` is **content-addressed** (`sha256` over `(endpoint_id, mode, spec)`); a `node` by its `principal_id`. Every foreign key stores the target's primary key, so a rename is free ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)).
- **A `location`, `system`, or `component` name is unique within its placement, not across the fleet**
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
Postgres statement with no `IF NOT EXISTS` form of its own, e.g. a column rename). Because the
version is the identity, nobody picks it by hand: `make migration NAME=<snake_case_name>` runs
`dbmate new`, which stamps the current UTC time. `make test` fails when two files share a version,
and the `migrations` CI workflow (locally, `make migration-check`) fails a change whose added
migration is not newer than the newest on `main`, the branch cut before another migration landed.

Migrations only move forward, so a binary refuses a schema newer than it knows. Before applying
anything, the `migrate` run mode, the server's boot and every direct-database command compare the
database's newest applied migration with the newest the binary embeds. A database a later build
migrated stops an older image with an error naming both versions (`ErrSchemaAhead`), rather than
booting on columns it does not know. An applied version below the binary's head that it no longer
embeds (a chain collapsed into one init, #388) is history, not a newer schema, and passes.

Migrators take turns. Two servers booting after one upgrade, or a server racing a `migrate` run,
would otherwise apply the same pending migration twice and the loser would fail on a duplicate
object. Every migrate holds a Postgres advisory lock (`migrate.LockKey`) from that guard through its
last migration, so the first run migrates and the others wait, then guard the schema the first one
left and find nothing to do. The guard sits inside the lock on purpose: a run that judged the schema
before waiting could boot an older image on a schema a newer one moved meanwhile. The wait is
bounded (five minutes, dbmate's default), and dbmate's own lock on the migrating connection still
keeps two runs' migrations apart if the outer lock's connection drops.

A change that both reshapes the schema and needs default rows for the shape to be usable never mixes
the two in one migration. Three buckets, never conflated:

- **Schema migrations** (`db/migrations/*.sql`, dbmate): pure DDL. No seed rows: a schema dump or a
  future squash silently drops any row a migration inserted, so a migration that seeds data is a landmine
  for whoever collapses the chain later.
- **Boot seed phase** (idempotent upsert on every server start, `internal/seed/*.yaml`): ship-with
  reference data, authoritative via `ON CONFLICT DO UPDATE` for a canonical catalog (a release can
  correct it, and can **withdraw** a value it once shipped, which is the half insert-if-absent cannot
  do at all). It lands every shipped row in the `og` namespace and upserts on `(namespace, name)`, so it never writes an operator's row: a shipped name an operator already holds lands beside theirs, and the search path (`<table>_in_effect()`, #1432) has a bare name reach the operator's. A shipped row's own references (a product's vendor, a child type's parent) resolve inside `og`. `location_type` seeds this way since
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
  gained the ability to read facts on OTHER rows: it is now a fleet-wide question the gateway
  answers, `PreviewLabelRecompute` returning nothing, so a write path nobody thought of fails it
  rather than a list nobody updated passing
  ([ADR-0100](/architecture/decisions/#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-fleet)).

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

The structural and template entities relate as shown on [core entities](/architecture/core-entities/) and [templates](/architecture/templates/); the collection entities (`transport` / `endpoint` / `task`) on [collection](/architecture/collection/#storage).

## Ground-truth records

The immutable, append-only records: the lineage targets and what a backtest reads, none derived. Columns for `audit_log` live on [audit](/architecture/audit/), `session_log` on [nodes](/architecture/nodes/#sessions):

- **`log_line`** (a **component's** own words, the untyped raw ingest lane, not a sample, component-only, [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)): every write is held at the Storage Gateway to RFC 5424's eight severities through one alias table (a word outside it kept as `severity_raw`) and to 16 KiB each for the message and the attributes, and carries `received_at`, the server's receipt, beside the time it claims ([ADR-1145](/architecture/decisions/#adr-1145-a-driver-emit-lands-on-any-of-the-four-lanes-and-a-log-lines-severity-is-rfc-5424s));
- **`node_log`** (a **node's** self-logs, the same payload shape without the owner arc, split by origin, [ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus));
- **`audit_log`** (operator actions: actor, verb, resource, `old -> new`; the lineage target for operator writes; secret decrypts always recorded, [audit](/architecture/audit/)).

:::design[Target design, tracked in #430]
- **`session_log`** (connection-lifecycle transitions, node-reported, [nodes](/architecture/nodes/#sessions)); the built session (#816) needed none: its state is the `session-state` property and its reasons are node logs ([ADR-0150](/architecture/decisions/#adr-0150-a-held-sessions-state-is-a-property-series-and-node-logs-not-a-session-table)), so this table waits on per-session counters earning it;
- **`internal_log`** (platform self-narration: startup / reconcile / migration / node-reg / config-sync, [workers](/architecture/workers/));
- the **`collection_log`** companion (the cheap per-run execution record).
:::

## The narrow metric lane

The metric lane stores every observation of a quantity, so its row is the one the volume is
made of, and most of a wide row was the series it belongs to, repeated on every sample
([ADR-1275](/architecture/decisions/#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month)).
The lane is three tables:

- **`metric_series`**: one row per series, keyed by an integer: the type, the owner arc (with
  `owner_id`, the arc's one filled column), the instance, the provenance, the source, and
  `collector_node_id`, the node whose task collected it. Its unique key leads with the owner, so
  it is also the **logical key**'s index: every reader asks for an owner's series of a type
  (`type, owner, instance, provenance`) and merges the series it finds, one per source and
  collecting node, so a task that moves to another node still reads as one series. Deleting an
  owner or a type removes its series (`on delete cascade`).
- **`metric`**, the hot tier: `(id, series_id, ts, value)`, plus a calculated sample's rule
  lineage in nullable columns that cost nothing when empty. Observed and calculated samples only.
  Range partitioned by month on `ts` from birth, with one btree, `(series_id, ts desc, id desc)`,
  and block range indexes on `id` and `ts`. It has no primary key and no foreign key to the
  series: a deleted owner's samples are invisible to every reader (each joins the series) and
  leave with their month or the next retention pass.
- **`metric_declared`**, the declared tier: a command's intended rows, with their command and
  event lineage, never pruned.

A batch lands its metric lane in one statement still ([ADR-1274](/architecture/decisions/#adr-1274-a-batch-lands-its-metric-lane-in-one-statement-except-a-series-it-carries-twice)):
the statement finds each sample's series, or starts it, and inserts the samples. A series'
newest sample is an index probe of each month's series index, newest month first, stopping at
the first that holds one. The `make footprint` harness measures the row at about 60 bytes of
heap and 40 of index after `REINDEX` (126 and 91 before).

**Partitions roll by the server.** The server keeps a partition for this month and the next two:
it checks at boot, before its ingest starts, and refuses to start when it cannot, and again on
every retention pass. A partition is created detached and then attached, so the parent is held
only at `SHARE UPDATE EXCLUSIVE`, under a five-second lock timeout. A sample whose month has no
partition (a late roll, a node clock far out) is not a refused write: the batch's months are made
and it lands, and a batch that misses again is redelivered, never dropped (#420). A month is UTC.

`property` is not partitioned: its rows are transitions, sparse by construction.

## The lineage CHECK (the pattern)

Lineage lives on the derived row, no separate execution table: a derived row *is* the evidence of its rule's run. The pointer per provenance is enforced, making "intended with no command" impossible at the storage layer. The real four-branch CHECK on `property` (on the metric lane the same rule is structural: provenance is the series', a calculated sample carries its rule, and an intended row lives in `metric_declared`, whose `command_id` is not null, so the hot tier has no command or event lineage to carry):

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
`property_series_idx` (`component_id, property_type_id, instance, ts desc`) for the component arc, and
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
| `session` | `session_log` | **view** | low-volume; node, endpoint, status, opened_at, last_activity_at, command/error counts. Not built: the current state reads as the latest `session-state` per endpoint (ADR-0150) |

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

## Retention: what is kept, and for how long

The server prunes the two sample tables to a window each
([ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks)):
`metric` to `retention.metric_days` (14 by default) and `property` to `retention.property_days`
(90), install-wide settings an admin changes on the console's Settings page (a platform-domain
namespace that any signed-in caller reads). `retention.trend_days` (730) waits for the hourly
trends of #1276. Each window is whole days from 1 to 36,500. The pass runs at server start, beside
the receipt prune, and again an hour later, or a minute later while a lane is behind.

Every prune goes through the **`PruneSamples`** gateway primitive, which carries the one
non-negotiable rule ([ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)):
it deletes series rows older than a cutoff **except** any `declared` or intended row (an operator's
assertion is the whole truth however old, not a sample) and, on the property lane, **except** the
latest row of every series (a transition-only series still measured may hold one row for months).
On the metric lane the floor is **the latest row within retention**
([ADR-1275](/architecture/decisions/#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month)):
a quantity stores every observation, so a series still measured always has a recent row, and one
with no row inside its window reads as no reading within retention. A calc rule's metric output
holding steady is written again once its newest row is twelve hours old, for the same reason.
Two refinements keep every
property reader whole:
- a row is the series' latest unless some row of its series has both a later-or-equal `ts` and a
  higher id, since the latest-value reads order by `ts` then id and the health reads by id, and a
  clock that stepped back (#801) makes the two disagree;
- a command's intended row (`command_id` set) is kept with the command, which is never pruned
  and reads its intended value back from that row.

Retiring a row the writer ruling refuses ([above](#every-key-has-one-writer)) is not a prune: it moves
the row out of service into `retired_sample` and keeps it, so nothing true is lost, which is the floor's
reason.

It is bounded so a large backlog never holds the database. A call deletes at most a chunk of
rows, each statement its own short transaction. The cutoff applies first. On the property lane a
statement takes one owner arc, and each old row's check for a newer row of its series is a scalar
subquery limited to one row, which stays a per-row index search (an `EXISTS` can be flattened into
a hash over the whole table) through `property_series_idx` for a component and the owner indexes
for a system or a location. On the metric lane a statement takes one month, oldest first, and
deletes the rows it chose by TID; a type with a window of its own walks its series and searches
each one's old rows through the series index. A pass removes at most 40 chunks of 5,000 rows a
lane and names any lane it left behind, and the next pass comes a minute later.

**A month past every window is dropped whole.** Before the chunks, the pass drops each metric
month that lies wholly before the metric window and before every type's own window, so a type kept
longer keeps its months. It detaches the month `CONCURRENTLY` (waiting for the readers already in it
rather than queueing every reader behind an exclusive lock) and then drops it, and finishes a detach
an earlier pass left halfway. The chunked prune keeps the window exact inside the months that remain.
On the property lane, finding the old rows reads every page the `ts` BRIN cannot rule out, which
reused pages widen over time: hence the hourly steady state.

The same pass keeps the two log tables, a component's `log_line` and a node's `node_log`, by
severity band (`logline.RetentionBands`): `retention.log_debug_days` (3 by default) for debug,
`log_info_days` (14) for info and notice, `log_warning_days` (30) for warning, and
`log_error_days` (90) for error, critical, alert and emergency. A line with no severity is kept with
info. A node's own logs may keep a band for a window of their own: `retention.node_log_debug_days`,
`node_log_info_days`, `node_log_warning_days` and `node_log_error_days` are unset by default, and while
one is unset `node_log` follows the shared band; once an admin sets one, the pass prunes that band of
`node_log` by it and `log_line` still by the shared window (#1350). A per-node window, through the
cascade with a node as its leaf, waits for #1351. A log line has no floor: it is not a series' value, and an event that named one as its source
keeps itself and drops the link (`event.source_log_line_id` is `on delete set null`).
- **Searched, not walked.** Each band's prune searches exactly the rows it may delete, through the
  table's `(severity, ts)` index, or its `(ts)` index for lines with no severity, ordered as that
  index is so the planner never prefers a sequential scan. The BRIN limit above does not apply.
- **The event link has its own index.** A deleted line's events keep themselves and drop the link
  through `event_source_log_line_idx`, so the foreign key's set-null never scans the event table.
- **One vocabulary.** Every stored severity is on RFC 5424's canonical set. A one-time backfill
  mapped the older words (`warn`, `ERROR`); a word outside the table moves to the `severity_raw`
  attribute. Both log tables normalize every new line at the gateway, so no band ever meets a
  stray word.

**A type can keep a window of its own**, overriding its lane's: a metric or property type's
`retention_days`, set through `POST /metric-types/{name}:setRetention` (and the property-type twin)
on an official type too, since a shipped type's definition is the seed's but how long an install keeps
its samples is the install's. The boot seed never writes it. The lane's own prune skips every
overridden type, and the pass gives each one a lane of its own, pruned to its window with the same
floor, chunking and index probe ([ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks)).

::::design[Target design: the metric lane is partitioned (#1275); the other append-only tables are tracked in #1417, and the `raw_sample` buffer in #430]

## Partitioning and retention

- **Append-only tables are range-partitioned by `ts`** (native declarative partitioning, rolled by the server: no `pg_partman`, no `pg_cron`). The firehose (`metric`) is the partitioning-critical one, and is built ([above](#the-narrow-metric-lane)); `event`, `log_line` and `node_log` are not yet. `property` is not partitioned: sparse by construction.
- **Retention is per table**, set by policy: `metric` short, `property` longer, `audit_log` longest (compliance), `internal_log` short, and `log_line` / `node_log` keyed on their own axes (`severity` / `facility` are indexed precisely so retention and routing can discriminate). On-row lineage ages out with its sample, and a `log_line` aged out from under an event leaves `event.source_log_line_id` null (`on delete set null`), never deleting the event. Per-table defaults are **cascade-resolved** ([cascade](/architecture/cascade/)) with an install-wide `platform` binding. Every path over the sample tables obeys the `PruneSamples` floor above; a log line has none. (Built for the sample lanes as one install-wide window per lane, and for the log tables as one per severity band (with an optional install-wide override per band for `node_log`, #1350), in the platform-domain `retention` namespace, not cascade-resolved: [ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks).)
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

### Every key has one writer

The gateway holds a second invariant beside scope, on the write side
([ADR-0161](/architecture/decisions/#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway)).
Scope says which owners a write may reach; the **writer class** says who may write a key at all. Every
metric, property and event type has exactly one, resolved from code and the catalogs and never stored,
so it cannot drift from what a write is held to (`internal/writerclass`; the type reads report it as
`writer_class`, and [metrics and properties](/architecture/properties/#who-writes-a-key-the-writer-class)
renders the ruling from the code). The two **reserved** classes, a node's own probe keys and the keys
only the Omniglass process writes, are declared in code and closed to an operator-created type on every
lane.

Every gateway function that writes a sample or an event row names its writer (the node lane bound to
the endpoint its task ran, the push lane, a calc rule's tick, the health rollup, a command, an
operator) and holds every row to its key's class (`admitWrites`) before anything lands, refusing the
write (`ErrKeyWriterRefused`, a 422 naming the key and who writes it) when the class does not admit it.
Like scope, it is structural rather than remembered per route: a source guard fails on a function that
writes a sample or event row without being a registered write path that runs the check, and a table
drives every registered path with a writer its key does not admit. The node lane's writer binds both
halves of what a node holds: the component its task collects for and the endpoint it ran against, so
every row of a batch lands on that component, and a node-probe row under that endpoint.

What was written or declared before the ruling is held to it as well
([ADR-1100](/architecture/decisions/#adr-1100-legacy-rows-and-stored-declarations-honor-the-one-writer-ruling)).
A one-time backfill **retires** every row of a reserved key whose class would refuse it today: the row
moves whole into `retired_sample`, with its lane, its id and the reason, so no read serves it and its
record survives (a flag would need every read to remember it; a move leaves none to forget). A declared
key pin is trusted only from the endpoint's audited `:retry`, whose row carries `source` `key-pin`; a
legacy `:retry` pin was recognized by its audit row and stamped. A stored driver spec or derived task
never hands a node an emit the ruling refuses (the worklist strips it), a contract cannot declare a
reserved key, and at boot the server judges again every event rule series a retirement took rows
from (so a condition a retired row raised lets go) and names every stored declaration the ruling
refuses so an operator can mend it.

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

## The standard's map

A standard may carry a room-layout declaration (ADR-0128): the `map` jsonb column, one
value holding an aspect ratio and normalized 1-based role positions, validated in Go on
write and cleared by a JSON null. Display data: nothing queries into it, every conforming
system renders from it.

## Query construction: typed, parameterized, generated

**Logic lives in Go, and the schema carries exactly one deliberate exception.** That exception is
the **owner-invariant** guard: `assert_owner_grant_exists()` and the `DEFERRABLE INITIALLY DEFERRED`
constraint trigger `principal_grant_owner_guard`, which refuse to leave the fleet with zero
`owner @ all` grants at `COMMIT` ([ADR-0006](/architecture/decisions/)). It is a trigger on purpose,
because the property it holds is true only at commit time and no application check can be: two
transactions each revoking the second-to-last owner grant both see one remaining.

Everything else composes at the gateway. The last stored function that was NOT that guard retired
with
[ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions):
what names a principal (a human's username, else a service account's name, else a node's name) is now declared once in
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
