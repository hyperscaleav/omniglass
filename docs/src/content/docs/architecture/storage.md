---
title: Storage
description: "How storage is built: Postgres as the system of record behind one Storage Gateway, dbmate migrations in three buckets, the narrow metric lane and its folded tiers, and retention."
sidebar:
  order: 60
  badge:
    text: Partial
    variant: note
---

Postgres is the system of record: entities, telemetry, events, alarms, audit, catalogs and
settings. Every read and write passes through one layer, the **Storage Gateway**, which injects
the caller's scope, writes the audit row, and holds each key to its one writer. This page covers the
patterns every other subsystem's tables land on. The columns themselves live with the feature that
owns them, rendered from the migrated schema: [samples](/architecture/properties/),
[events](/architecture/events/), [audit](/architecture/audit/), [files](/architecture/files/), and
[identity and access](/architecture/identity-access/#storage).

Postgres is **bring your own** today: an install points the server at a database it runs.

:::design[Embedded Postgres, tracked in #19]
The all-in-one run mode brings Postgres up in-process as a managed subprocess, a real Postgres (so
partitioning, JSONB and the CHECK constraints behave as they do at scale), never SQLite.
:::

## The Storage Gateway and tiering

`internal/storage` is the only package that may open a database connection. A guard test
(`internal/build/import_boundary_guard_test.go`) fails on any non-test file outside it that imports
`pgx` or `database/sql`, except three named schema tools (`internal/erd`, `internal/compressbench`,
`cmd/erdgen`), and fails again if an allowlisted directory stops needing its entry. A second guard
keeps the outpost binary from importing the storage or API packages at all. There is no row-level
security and no PostgREST: the gateway is the only door, and the API is its only caller.

The gateway holds the second of the two authorization layers. Every scoped method takes the caller's
resolved scope as an explicit `scope.Set` argument, one per action it performs (a read scope and,
for a write, the action's own), and turns it into a parameterized row filter on the row's owner. A
caller cannot forget to pass it: it is a required argument, and seed and system callers pass an
all-scope set on purpose. A write whose scoped predicate matches no row is never a silent success:
it surfaces as the 403 or 404 the [three-way status split](/architecture/identity-access/#the-three-way-status-split)
owes. Every mutation writes its [audit](/architecture/audit/) row in the same transaction as the
change.

The gateway also takes a **query tracer** (`storage.WithQueryTracer`) on the pool's connection
config, so it sees every statement whichever method issued it. The server installs
`storage.SlowQueryTracer`, which counts and logs any statement slower than `database.slow_query_ms`,
and the test harness counts round trips through the same seam, which is how a list is held to a
constant number of queries rather than one per row
([counting round trips](/contributing/test-driven/#counting-round-trips-not-timing-them)).

Opaque bytes (a firmware image, a configuration dump) live in the content-addressed
[blob store](/architecture/files/), a `blob.Store` seam behind the same gateway. The built backend
holds the bytes in Postgres, and a row references a blob by its `sha256`, never inline.

:::design[Alternate blob backends, tracked in #248]
An S3-compatible or disk backend swaps in beneath the same `blob.Store` seam with no model change,
since rows reference blobs by hash.
:::

:::design[The cold tier and typed query construction, tracked in #529]
Aged metric and log partitions tier out to a columnar or object store behind the same gateway, so a
historical query fans across hot and cold with no model change. Queries are built with a type-safe
builder generated from the dbmate schema, so a wrong column name is a compile error rather than a
reviewed convention; today every statement is hand-written SQL with bound parameters.
:::

### Every key has one writer

Scope says which owners a write may reach. The **writer class** says who may write a key at all
([ADR-0161](/architecture/decisions/#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway)).
Every metric, property and event type has exactly one, resolved from code and the catalogs and never
stored (`internal/writerclass`), and the type reads report it as `writer_class`
([who writes a key](/architecture/properties/#who-writes-a-key-the-writer-class)). Two classes are
reserved: an outpost's own probe keys, and the keys only the Omniglass process writes. An operator
cannot create a type in either.

Every gateway function that writes a sample or an event names its writer (an outpost's task, the
push lane, a calc rule, the health rollup, a command, an operator) and admits every row against its
key's class (`admitWrites`) before anything lands, refusing the write with `ErrKeyWriterRefused`, a
422 naming the key and who writes it. Like scope it is structural: a source guard fails on a
function that writes a sample or event row without being a registered path that runs the check. An
outpost's writer binds both halves of what it holds, the component its task collects for and the
endpoint it ran against, so every row of a task lands on that component.

Rows written before the ruling are held to it too
([ADR-1100](/architecture/decisions/#adr-1100-legacy-rows-and-stored-declarations-honor-the-one-writer-ruling)).
A one-time backfill moved every reserved-key row the ruling refuses into `retired_sample`, with its
lane, id and reason, so no read serves it and its record survives.

## Conventions

- **Identity is three columns.** `id` is a uuid: immutable, the primary key, and what every foreign
  key stores ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)).
  `name` is the renameable identifier an operator types; `storage.ValidateName` is the one validator
  for it. `label` is an optional friendly string. A rename moves only `name`.
- **A location, system or component name is unique within its placement**, not across the fleet: a
  set of partial unique indexes, one per placement bucket, so a dotted address resolves one hop per
  segment
  ([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup);
  scope then narrows a bare name, see
  [a reference resolves within a scope](/architecture/identity-access/#a-reference-resolves-within-a-scope)).
- **No `tenant_id`.** A tenant is a database: there is no tenant column anywhere
  ([scaling](/architecture/scaling/#multi-tenancy-per-database-per-account-per-deployment)).
- **A catalog key is a namespace and a name**
  ([ADR-0318](/architecture/decisions/#adr-0318-every-catalog-key-is-a-namespace-and-a-key)). `og`
  is the platform's, written only by the boot seed and always `official`; `local` is an operator's
  default. An operator's edit of a shipped row forks it into `catalog_shadow`, keyed on the shipped
  row's own uuid, and reads resolve the fork over the shipped row, so no foreign key or URL learns
  about it ([ADR-0095](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)).
- **Ownership is an exclusive arc**: an owner kind plus one typed foreign key and a CHECK that exactly
  one is set ([core entities](/architecture/core-entities/#ownership-the-exclusive-arc)).
- **Lineage lives on the derived row.** A property row carries its provenance (`observed`,
  `calculated`, `intended`, `declared`) and a CHECK enforces which lineage pointer each must carry: an
  intended value names its command, a calculated one its rule. A derived row is the evidence of the
  run that wrote it; there is no separate execution table.
- **A write struct takes the `Write` suffix**, the bare noun is the row (`MetricSampleWrite` in,
  `MetricSample` back)
  ([ADR-0072](/architecture/decisions/#adr-0072-an-envelope-is-not-named-after-its-passengers-and-an-insert-struct-takes-the-write-suffix)).
- **Logic lives in Go.** The schema carries one deliberate exception: the owner invariant, a
  deferred constraint trigger that refuses to commit a database with no `owner @ all` grant, since
  only commit time can see two transactions each revoking the second-to-last one
  ([the owner invariant](/architecture/identity-access/#the-owner-invariant)).
- **Current state is derived on read.** `alarm` and `action` hold their state in real tables;
  everything else "current" (a series' latest value, a system's health) is an indexed read of the
  series, with no SQL view and no maintained cache in the schema. A maintained table has to be earned
  by a measured read profile, and each bulk read is held to its index by an access-path assertion
  ([ADR-0094](/architecture/decisions/#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing)).

## Migrations: three buckets, kept separate

Schema is managed with **dbmate**: migrations under `db/migrations/`, embedded into the binary and
applied by the `migrate` run mode. A migration runs exactly once (dbmate keys on its timestamp
version, not its contents), so an applied migration is never edited, only followed by a new one, and
its DDL is idempotent. `make migration NAME=<snake_case_name>` stamps the version; `make test` fails
on two files sharing one, and `make migration-check` fails a migration not newer than the newest on
`main`.

Migrations only move forward, so a binary refuses a schema newer than it knows: `migrate`, the
server's boot and every direct-database command compare the database's newest applied version with
the binary's own and stop with `ErrSchemaAhead`, naming both. Migrators take turns: every run holds a
Postgres advisory lock (`migrate.LockKey`) from that check through its last migration, so two servers
booting after one upgrade apply each migration once.

A change that reshapes the schema and needs rows for the shape to be usable never mixes the two:

- **Schema migrations** (`db/migrations/*.sql`): pure DDL, no seed rows, since a schema dump or a
  squashed chain silently drops any row a migration inserted.
- **The boot seed** (`internal/seed/*.yaml`, on every server start): ship-with reference data,
  upserted into the `og` namespace on `(namespace, name)` with `ON CONFLICT DO UPDATE`, so a release
  can correct or withdraw a shipped row and never writes an operator's. Two tables with no operator
  write path, whose leftover rows would not sit inert, reconcile to the declared set and delete what
  dropped out: `choice_alternate`
  ([ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability))
  and `reserved_key`, which mirrors the names `writerclass` reserves.
- **One-time data backfills** (dbmate, data only): transform existing operator rows to meet a new
  constraint, run once, and change nothing on a second run.

The usual order is nullable column, then the seed and backfill that make every row satisfy the
coming constraint, then the constraint. A fresh database and an upgraded one converge on the same
end state.

## The narrow metric lane

The metric lane carries every observation of a quantity, so its row is the one the volume is made
of. It is narrow
([ADR-1275](/architecture/decisions/#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month)):

- **`metric_series`**: one row per series, keyed by an integer: the type, the owner, the instance,
  the provenance, the source and the collecting outpost. Its unique key leads with the owner, so it
  is also the index every reader asks through.
- **`metric`**, the hot tier: `(id, series_id, ts, value)`, plus a calculated sample's rule lineage.
  Range partitioned by UTC day on `ts`
  ([ADR-1527](/architecture/decisions/#adr-1527-the-metric-lane-is-partitioned-by-utc-day)), with one
  btree on `(series_id, ts desc, id desc)` and block range indexes on `id` and `ts`.
- **`metric_declared`**: a command's intended rows, with their command lineage, never pruned.

**Partitions roll by the server.** At boot, before ingest starts, and on every retention pass, the
server keeps a partition for yesterday, today and the next three days, then reads them back and
refuses to start ingest unless they cover that span. A sample whose day has no partition is not
refused: its day is made and the batch lands. A partition is created detached and then attached, so
the parent is held only briefly. `property` is not partitioned: its rows are transitions, sparse by
construction.

**Hourly trends.** `metric_trend` holds one row per observed series per UTC hour (count, minimum,
maximum, sum, first, last), so a chart beyond the hot window still draws every hour
([ADR-1276](/architecture/decisions/#adr-1276-hourly-trends-close-an-hour-a-day-after-it-ends-and-hold-back-the-prune)).
The retention pass closes each hour a day after it ends, behind two marks (announced, then
compacted) and an advisory lock that every metric insert reads, so a pass interrupted anywhere skips
no hour and a late sample recomputes its hour. A read wider than 48 hours is raised to whole hours and
takes its closed hours from the trends
([ADR-1566](/architecture/decisions/#adr-1566-a-wide-bucketed-read-is-whole-hours-and-takes-closed-hours-from-the-trends)).

**Lossless blocks.** Each closed day of a series also folds into `metric_block`: one row per span,
the samples' millisecond offsets and values in two arrays, every sample exact, about 7 to 9 bytes a
sample against about 133 for a raw row
([ADR-1528](/architecture/decisions/#adr-1528-observed-metric-samples-fold-into-lossless-blocks-sized-by-their-sample-count)).
The span follows the sample count: a dense day is cut into hour blocks, a moderate one is one day
block, and a sparse week folds into one week block. A block carries one rule lineage, so a calculated
series splits where its rule was edited
([ADR-1534](/architecture/decisions/#adr-1534-calculated-metric-series-fold-into-blocks-that-carry-their-rule-lineage)),
and a late sample merges into the block that covers it
([ADR-1529](/architecture/decisions/#adr-1529-a-late-metric-sample-merges-into-the-block-that-covers-it)).
A day folds only once its own close and the trend horizon have passed, so the shortest metric window
that folds is three days, and a shorter one is refused at every write path.

**One series read.** Every reader of a series' samples (the raw and bucketed reads, the API and the
CLI, calc inputs, the event rule judge, rule preview and backtest, settlement, the current value)
goes through one gateway read that takes a folded day from its blocks and the rest from raw rows, so
each answers the same before a day folds and after
([ADR-1530](/architecture/decisions/#adr-1530-a-series-read-takes-a-folded-day-from-its-blocks-and-the-rest-from-raw-rows)).
`TestNoDirectMetricReads` fails on any read of the raw table outside a short allowlist, each entry
with its reason.

:::design[Raw rows drop after the fold, tracked in #1574]
The blocks are written beside the raw rows, which no pass drops yet. Once the early drop lands, raw
rows live about two days and history is the blocks, kept for the metric window.
:::

## Ingest lands a batch in one transaction

An outpost publishes one batch a tick, a group per task. The ingest consumer lands every lane of it
(`metric`, `property`, `event`, `log_line`, `outpost_log`) in **one transaction** whose first
statement inserts the batch's receipt (`telemetry_receipt`), keyed by the subject and the batch's
`Nats-Msg-Id`. A batch that already landed lands nothing and counts one more delivery, so a
redelivery lands each row once
([ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).
The receipt also carries the batch's accounting, what became of every item, which is how a loss is
read ([accounting](/architecture/messaging/#accounting-every-item-the-loss-ratio)). Receipts are
pruned past a day.

The transaction's statement count does not grow with the groups: the gateway preloads every owner's
arcs once, admits each group against its writer in Go, takes its locks once over the union, and
writes each lane as set inserts
([ADR-1928](/architecture/decisions/#adr-1928-a-telemetry-batch-reads-and-locks-once-and-stamps-seen-inside-its-transaction)).
A value only Postgres refuses (an overflow, a CHECK) sends the batch through an isolate pass, one
savepoint per group, to find the group to drop; the batch then lands without it.

:::design[The two-lane data plane, tracked in #2611]
The samples tables become an asynchronous sink behind a persistence consumer, and every row born in
a Postgres transaction is fanned onto the bus by a leader-elected CDC publisher reading the WAL
([messaging](/architecture/messaging/#two-lanes-one-bus)). Today the ingest lanes confine and persist
inline.
:::

## Retention: what is kept, and for how long

The server prunes each telemetry lane to a window, set in the `retention` settings namespace (any
signed-in caller reads it; an admin writes it on the [Settings page](/admin/settings/))
([ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks)):

| Lane | Setting | Default (days) |
| --- | --- | --: |
| metric samples | `retention.metric_days` | 14 |
| property transitions | `retention.property_days` | 90 |
| hourly trends | `retention.trend_days` | 730 |
| debug log lines | `retention.log_debug_days` | 3 |
| info and notice log lines | `retention.log_info_days` | 14 |
| warning log lines | `retention.log_warning_days` | 30 |
| error and worse log lines | `retention.log_error_days` | 90 |

A metric or property type can keep a window of its own (`POST /metric-types/{name}:setRetention`
and the property twin), which the boot seed never writes. An outpost's own logs may keep a band of
their own (`retention.outpost_log_debug_days` and its siblings), unset by default, and follow the
shared band while unset
([ADR-1350](/architecture/decisions/#adr-1350-a-node-log-window-is-an-optional-setting-unset-follows-the-shared-band)).
The pass runs at server start and hourly after, or a minute later while a lane is behind.

Every sample prune goes through **`PruneSamples`**, which carries the floor
([ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)):
it never deletes a declared or intended row (an operator's assertion is the whole truth however old),
and on the property lane never the latest row of a series, since a transition-only series still
measured may hold one row for months. On the metric lane the floor is the latest row within the
window. A log line has no floor; an event that named one as its source keeps itself and drops the
link.

A prune is bounded so a backlog never holds the database: each statement deletes at most 5,000 rows
in its own short transaction, and a pass spends at most 40 chunks a lane. Before the chunks, a metric
day wholly past every window that reaches it, and past the trend's compacted mark, is detached
concurrently and dropped whole
([ADR-1957](/architecture/decisions/#adr-1957-the-metric-lanes-prune-leaves-a-droppable-day-to-the-drop)).

:::design[Partitioning the other append-only tables, tracked in #1417]
`event`, `log_line` and `outpost_log` are range-partitioned by `ts` like the metric lane, so their
retention is a dropped partition rather than row deletes.
:::

:::design[A per-outpost log window, tracked in #1351]
An outpost's log window resolves down the cascade with the outpost as its leaf, rather than one
install-wide value per band.
:::

### What a retention pass reports

Each pass lands its figures as platform metrics on the server's own component in the platform
system, written as the process, so no outpost, push or operator can write them (the keys render on
[collection](/architecture/collection/#self-monitoring-every-outposts-account-of-itself)):

- **`retention-deleted-count`** and **`retention-behind`** per lane the pass pruned, the instance the
  lane's name (`metric`, `property`, `metric_trend`, `metric/<type>`, `log_line/info`). A lane that
  drops out of a pass while flagged is written 0, so its alarm clears.
- **`retention-behind`** for the trend and block compactions, and **`retention-window-days`**.
- **`retention-pass-duration`**, **`retention-pass-failure-count`**, and the partitions the pass
  created and dropped.
- **`metric-trend-mark-lag`** and **`metric-block-mark-lag`**: a healthy trend mark trails by a day
  and an hour or so, a healthy block mark by one to two days.
- From one status read: the partition count, the oldest partition's age, and **`table-bytes`** per
  stored table.

**Behind and failed read differently.** A lane is behind when the pass stopped at its chunk budget
with rows still past the window, normal after an outage or a shortened window. A failure is an error,
counted in `retention-pass-failure-count`, and a fault to read in the server log. The seeded
**`retention-behind`** rule opens a warning alarm on the server's component when a lane stays flagged
for five minutes, and the first pass that reads it 0 clears it. Beside the figures, a pass writes what
it did as `log_line` rows on the same component (facility `retention`), one per kind of work, and a
pass that did nothing writes nothing
([ADR-1708](/architecture/decisions/#adr-1708-the-platforms-self-narration-is-log-lines-on-its-platform-component-not-a-table-of-its-own)).
