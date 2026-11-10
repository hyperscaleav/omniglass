---
title: Scaling and deployment
description: "One binary that runs a laptop demo or a Kubernetes fleet: two run modes, embedded Postgres and NATS, the CDC bridge, horizontal scale, high availability, platform configuration, and per-database multi-tenancy."
sidebar:
  badge:
    text: Partial
    variant: note
---

Omniglass is **one Go binary**, and that is a packaging decision, not a scale ceiling. The same artifact
runs an all-in-one container on a laptop and a horizontally-scaled fleet on Kubernetes; you scale by
**topology**, not by swapping products.

:::note[Partial]
Built today: the single Go binary (hand-written commands: `server`, `migrate`, `bootstrap`, `token`,
`set-password`, `seed-dev`, and `node run`), the embedded NATS server (in-process `nats-server` with
JetStream, `internal/cli/server.go`), the `node` run mode, the multi-arch container image, and the
Helm chart. Today one stream and one serial consumer do confinement and persistence inline; the
design fences below mark the parts of this page that are target design, each naming its tracker. See
[implementation status](/architecture/status/).
:::

## Two run modes, one binary

The binary is a **modular monolith**: one codebase, one artifact, modules behind clean seams (the Storage
Gateway is the only path to the database, coordination rides NATS, collection runs at the edge). It runs
two ways, no fork:

- **All-in-one (the modular monolith).** One process runs every role, with **Postgres and NATS embedded**
  (below), against nothing external. The small-fleet case: download, run, done.
- **Split by run mode (Kubernetes).** The same binary launched **per mode** as separate Deployments,
  against an **external** Postgres and an external NATS cluster. A Helm chart wires it up, and each role
  scales independently.

Splitting a mode onto its own pods is a **deployment choice, not a rewrite**: the modules already talk
over NATS and the gateway. The roles:

- **server**: the public HTTP API ([API](/architecture/api/)) and the views read path; it serves the
  **SPA embedded in the binary** (`go:embed`), so the web UI is not a separate service. Stateless.
:::design[Target design, tracked in #57]
- **worker**: the **JetStream consumers** (rule engine, reconcile, notify, [workers](/architecture/workers/)).
  Stateless competing consumers.
- **controller**: the leader-elected **singletons** (the clock and the CDC publisher, below). A role, not
  necessarily its own pod.
:::

- **node**: collection; runs **at the sites** (outside the cluster) and connects back, plus a central
  `node` for cloud-API and SaaS sources (`placement: central`, [nodes](/architecture/nodes/)).

## Embedded services (single-binary mode)

Until embedded Postgres lands, the one-command way to run omniglass on a laptop is the
[lab](/guides/lab/): Docker runs Postgres, the server and a node with its simulated fleet as three
containers of one project. It is a place to learn, not the all-in-one deployment this section
describes: its data is disposable, its login is well known, and its database refuses a real server.

In all-in-one mode the binary brings its dependencies up in-process, so an operator runs **one container,
zero external setup**:

- **NATS + JetStream, embedded as a library** (`nats-server` in-process, file-backed). The app is always a
  NATS client; embedded versus external is a config flag, not a code path.
:::design[Embedded Postgres, tracked in #19]
- **PostgreSQL, embedded as a managed subprocess** ([embedded-postgres](https://github.com/fergusstrange/embedded-postgres)):
  a **real** Postgres, so logical decoding (the CDC bridge below), JSONB, partitioning, and the
  exclusive-arc CHECK constraints behave identically to at-scale. Pinned to **Postgres 18.3.0 or newer**
  for ARM and x86. **Not SQLite**, which has no logical replication and would fork the data layer into a
  second, lesser architecture.
:::

## Coordination: NATS moves, Postgres remembers

**Postgres is the relational system of record** (entities, samples, events and
alarms, audit, and the queries the cascade, fusion, views, and scope need). **NATS (JetStream) is the
nervous system**: work distribution, the durable command queue, the telemetry buffer, and fan-out.

:::design[The JetStream KV and object store, tracked in #529]
JetStream **KV** (config, locks, leader-election) and an **object store** for internal artifacts ride
the same substrate; user files stay on the content-addressed blob store (built: `internal/blob`,
Postgres-backed), not the object store.
:::

:::design[Target design, tracked in #430]
The two meet through **change data capture**: a single **leader-elected CDC publisher** bridges
committed Postgres changes onto JetStream; **Postgres is never a message bus**, it only emits its
changes. The mechanism is described once, on
[messaging](/architecture/messaging/#two-lanes-one-bus).
:::

:::design[Target design, tracked in #430]

### Inter-service communication

Service-to-service traffic rides **two lanes on the one JetStream bus**: the
NATS-native **data lane** for samples, and the Postgres-first, CDC-out **record and state lane** for
events, alarms, actions, and operator mutations. The lane model is described once, on
[messaging](/architecture/messaging/#two-lanes-one-bus); here is how the pieces deploy:

```d2
direction: down
classes: {
  node: { style.border-radius: 8 }
  key: { style: { border-radius: 8; bold: true } }
  group: { style.border-radius: 8 }
}
north: "North plane: public API (HTTP / AIP)" {
  class: group
  direction: right
  c1: "SPA" { class: node }
  c2: "CLI" { class: node }
  c3: "MCP / AI agent" { class: node }
  c4: "integrations · webhooks" { class: node }
}
binary: "one Omniglass binary: modular monolith (1..N replicas)" {
  class: group
  api: "API / server (per-replica)" { class: node }
  gw: "Storage Gateway (the only DB path)" { class: node }
  wk: "JetStream consumers: rule engine · reconcile · notify · persistence (per-replica, competing)" { class: node }
  clk: "clock (singleton)" { class: node }
  cdc: "CDC publisher: WAL to JetStream (singleton)" { class: node }
  nats: "embedded NATS: JetStream · KV · Object store" { class: key }
}
pg: "PostgreSQL: system of record" { class: node; shape: cylinder }
edge_nodes: "edge nodes (distributed · NATS clients)" { class: node }
ext: "external NATS cluster (optional BYO at scale)" { class: node }
north -> binary.api: "HTTPS"
binary.api -> binary.gw
binary.gw <-> pg
pg -> binary.cdc: "WAL (logical decoding)"
binary.cdc -> binary.nats: "publish committed changes"
binary.nats -> binary.wk: "east-west: work + events"
binary.wk -> binary.gw
binary.clk -> binary.nats: "schedule fires"
binary.nats <-> edge_nodes: "South: telemetry up · commands down" { style.stroke-width: 3 }
binary.nats -- binary.clk: "KV: config · locks · leader-elect" { style.stroke-dash: 4 }
binary.nats -- ext: "swap embedded for BYO" { style.stroke-dash: 4 }
```

:::

:::design[Target design, tracked in #57]

## Horizontal scale: what replicates

- **server** is **stateless**: replicate it behind a load balancer; state lives in Postgres.
- **workers** are **JetStream consumers**: a work-queue stream delivers each message to exactly one
  consumer, so adding replicas adds throughput (NATS is the coordinator,
  [workers](/architecture/workers/)).
- **edge nodes**: one or many per site, connecting back; adding sites adds nodes
  ([nodes](/architecture/nodes/)).
- **singletons** (the clock and the CDC publisher): **leader-elected via a NATS KV lock**, exactly one
  active, failover by re-election. No separate election service.
- **the control-plane consumers** (worklist, heartbeat) are today **singletons by construction**:
  one server holds each per-verb wildcard subscription
  ([ADR-0081](/architecture/decisions/#adr-0081-the-control-plane-wire-is-one-subject-grammar-node-anchored-and-batch-granular)).
  The worklist subscription feeds a bounded pool of workers on that server, so pulls are built in
  parallel without a second subscriber
  ([ADR-1577](/architecture/decisions/#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting)).
  The HA fork, queue groups versus worklist reassignment, is named and deferred to the day a
  second server exists; a second server is also a trigger for the materialized worklist (#1586).

:::

## Ingest: one batch per tick

What a node costs the server is set by how many transactions its telemetry makes, not by its sample
count. Before, a node published one batch per task per tick, and each batch landed in its own
transaction: about twenty statements (begin, receipt, writer lookup, arcs, locks, the database clock,
the targeted-property read, rules, marks, a property lock and insert, a metric insert, judged, commit,
plus the task-owner lookup and the endpoint stamp), at about one sample a batch. That is `20 x G`
statements a tick for `G` tasks, and one receipt row per sample, so the receipt table grew as fast as
the metric table.

Now a node publishes one batch per tick, a group per task
([nodes](/architecture/nodes/#delivery-a-batch-lands-once)), and it lands in one transaction
([storage](/architecture/storage/)) whose cost is a fixed `C1` (about twenty statements) plus `R`
(series the batch carries twice), `J` (judged samples that fire a rule), `T` (settled targeted
properties) and three per conduit group that carries identified samples. With none of those it is
the same for one group and for fifty. Before the transaction, the identity bindings of every conduit
in the tick resolve in one read, however many conduits its groups name. A refused group costs its siblings nothing
([ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).

| At 1,000 systems, the busiest node (18 nodes, about 3,000 tasks) | Before | After (target) |
| --- | --- | --- |
| Samples per batch | about 1 | 40 or more |
| Statements per sample | about 18 | about 0.5 |
| Batches a second, all nodes | one per task per tick | 18 or fewer (one flush a second per node) |
| Receipts a day | grows with the sample count | under 0.5 GiB |
| Ingest lag p99 | | under 5 s |

The right-hand column is the target this slice is held to, not a measurement. `omniglass lab measure`
prints the measured figures (batches landed, samples per batch, batches a second, receipts a day, WAL
bytes a sample, the ingest lag at p50, p95 and p99, consumer redeliveries, and the ingest consumer
lane's busy fraction and service time a batch) for a scale lab
([the lab guide](/guides/lab/#measuring-a-scale-lab)); the measured run replaces this column's targets
when the slice's pull request records it. The lane's busy fraction is the figure that says whether the
single consumer is the ceiling: the stream is a work queue with one message in flight, so the handler's
share of the wall clock is the lane's utilisation, and a lag that sits at the node's one-second spool
flush while the lane is mostly idle is not a consumer bottleneck
([#1517](https://github.com/hyperscaleav/omniglass/issues/1517)).

A faster lane is only a win if it still lands everything, so the same run reads every telemetry
kind's **loss ratio** across the node hop and the ingest hop, `(in - out) / in` with 0 healthy, from the ledger each
batch carries on its receipt
([messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio), #1641). It is the one
delivery measurement for every kind and hop: a change that gets faster by dropping data reads a loss
above 0 instead of a win, and the perf gate will fail such a run (#1695). The accounting rides on the receipt
rather than on new rows, and its cost shows in the receipts' own figure: on a three-system corporate
lab the receipt grew from about 290 to about 675 bytes a batch (receipts a day 56 to 129 MiB at about
2.3 batches a second, WAL a sample 400 to 474 B), part of it the dead tuple the outcome update leaves
until vacuum. At the 18 batches a second the table above expects of 1,000 systems, that is about
1 GiB a day where the narrow receipt read about 0.4: past the 0.5 GiB target, which predates the
ledger. Receipts are pruned at a day, so this is the table's standing size, not unbounded growth;
narrowing the receipt or revising the target is
[#1699](https://github.com/hyperscaleav/omniglass/issues/1699).

### Scale up or scale out: what the figures say

A running server reports the figures that make this call on its own platform component, every report
window (#1628; [messaging](/architecture/messaging/#measuring-the-pipeline),
[workers](/architecture/workers/#measuring-the-ticks)), so the evidence is the fleet's own history,
not a lab run. Read them together:

- **The lane is the ceiling.** `ingest-lane-busy` near 1, `bus-consumer-pending` growing window over
  window and `ingest-lag-max` climbing: the one consumer with one batch in flight is saturated. A
  bigger box helps only as far as it shortens each batch's landing (a faster database, a lower
  `db-pool-wait-duration`); past that the lane needs to run in parallel, which waits on sequencing per
  publisher ([workers](/architecture/workers/#one-machinery-several-consumers)); #1517 measures where
  the ceiling sits in a scale lab. This is the scale-out signal.
- **The database or the process is the ceiling.** `db-pool-wait-count` nonzero with
  `db-pool-acquired` at `db-pool-max`, or `process-cpu-seconds` divided by the window near the cores
  the server has, while the lane is not saturated: the server is starved, not the design. Scale up (more
  cores, a larger pool, a faster database) before adding a replica that would share the same database.
- **The lag is not the server's.** `ingest-lag` high while `ingest-lane-busy` is low and the backlog
  near 0: the samples arrive late (a node's spool holding through an outage, a slow WAN) or the node's
  clock is behind the server's. The lag is commit time minus the node's `ts`, two clocks
  subtracted, so a skew reads as lag and a negative lag is a node clock ahead; fix the clock, not the
  server (#419).
- **A tick is the ceiling.** `calc-pending-depth` growing while `calc-pass-duration` nears its second,
  or `outbox-due-age` growing, says that tick, not ingest, has fallen behind.

The seeded `ingest-lag-high` rule opens a warning alarm on the server's component when
`ingest-lag-max` holds over 30 s for 2 minutes, the point at which a reader of any panel is looking at
data at least half a minute old.

## Platform configuration

Configuration is **two tiers**, and platform settings are deliberately **centralized**:

- **Bootstrap (env, optional).** The irreducible minimum needed before the database exists: the Postgres
  DSN, the NATS embed-or-external choice and address, the `SecretProvider` key, the run mode, and the
  listen address. In all-in-one mode these have working defaults, so a desktop run needs **no configuration
  at all**; env vars override.
- **The platform settings store (one place).** Everything else lives in the single, audited
  [settings engine](/architecture/settings/): feature flags, buffer and retention defaults, CDC routing,
  integration settings, UI defaults, official-registry overrides, resolved most-specific-wins down the
  principal hierarchy. An operator **settings file** (`settings.json` or YAML) is the GitOps layer, read
  into memory at boot and mounting cleanly as a **Kubernetes ConfigMap** (and a future operator); only
  the **override** an operator sets through the API is persisted in Postgres (audited), while the file
  and the embedded defaults are recomputed each boot, so restore is a delete and the file never drifts
  into a second authoritative copy
  ([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)).

This is distinct from fleet [config and variables](/architecture/variables/), which describe the
*fleet* and resolve down the cascade; the settings store describes the **platform itself**.

:::note[Partial: the settings engine's platform level]
The settings store is built at the **platform** level (the install-wide rung, renamed from `global` by
[ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)):
the pure resolver, the `setting_override` table, the
admin API, the two `settings:<action>` permissions, and the Admin settings page, with `ui.theme` wired
through to re-theme the SPA (see [settings](/architecture/settings/)). The **group** and **user** cascade
rungs, and the `platform`-domain settings (CDC routing, integrations) named above, are the
fast-follow; `retention` has landed with sample retention ([ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks)).
:::

:::design[Target design, tracked in #57]

## Vertical scale and high availability

Replicas are the **HA** story: the server and worker tiers have no single point of failure, the
singletons re-elect on the NATS KV lock, Postgres HA is the database's concern (CNPG, a managed
cluster), NATS HA is the JetStream cluster's, and the **edge survives a WAN outage on its own** (the
bounded buffer plus the durable command queue, [nodes](/architecture/nodes/)). Vertical scale is the
simple first lever; horizontal removes the ceiling.

:::

## Multi-tenancy: per database, per account, per deployment

Tenant isolation is **physical, not a row predicate**: a tenant is **one database, one NATS account, and
one deployment**. There is no `tenant_id` column anywhere, no shared row store, and no shared subjects, so
per-database isolation (storage) and per-account isolation (messaging) are the **same boundary**. The data
model stays single-tenant-shaped; multi-tenancy lives at the orchestration layer (CNPG-per-tenant). One
noisy or compromised tenant cannot reach another: there is nothing shared to reach across
([identity and access](/architecture/identity-access/)).
