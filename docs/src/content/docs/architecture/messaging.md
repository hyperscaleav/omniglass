---
title: Messaging
description: "The bus between the server and its outposts: the subject grammar and its grants, the telemetry stream and how a batch lands once, the loss ratio that accounts for every item, and the figures the server measures itself by."
sidebar:
  order: 34
  badge:
    text: Partial
    variant: note
---

Omniglass has two typed contracts. The [public API](/architecture/api/) is HTTP. This page is the
other one: a **NATS subject contract** over JetStream, carrying the server's traffic with its
outposts. The server embeds the NATS server (`internal/bus`), so there is no broker to run.
**Postgres is the system of record; the bus only moves.** Operators never connect to the bus: the
console reads over HTTP, live included ([below](#the-live-console)).

## Subjects and grants

Every control-plane subject is `og.v1.<verb>.<outpost>`, the outpost's name the last token and
exactly one token
([ADR-0081](/architecture/decisions/#adr-0081-the-control-plane-wire-is-one-subject-grammar-node-anchored-and-batch-granular)).
That lets the server subscribe one wildcard per verb (`og.v1.worklist.*`) and lets an outpost's
credential be an explicit allow-list of its own subjects (`outpostPermissions` in `internal/bus`):

| Verb | Direction | What it carries |
| --- | --- | --- |
| `worklist` | outpost asks, server answers | the outpost's whole worklist, gzipped past 64 KB ([outposts](/architecture/outposts/)); an endpoint-by-endpoint sync against a manifest is the planned design ([ADR-1586](/architecture/decisions/#adr-1586-a-node-syncs-its-worklist-endpoint-by-endpoint-against-a-manifest), #2606) |
| `worklist-changed` | server to outpost | an empty nudge to pull now, sent when a commit changes the outpost's worklist ([outposts](/architecture/outposts/#the-change-nudge)) |
| `heartbeat` | outpost to server | the outpost's check-in |
| `telemetry` | outpost to stream | each tick's `TelemetryBatch` |
| `command`, `commandstatus` | outpost asks, then reports | the outpost's command queue pull and its execution reports ([commands](/architecture/commands/)) |
| `run` | server asks, outpost answers | an on-demand run of one task |
| `simlist`, `simhit`, `simop` | both ways | the [simulator](/architecture/simulation/) beside the outpost, which holds its credential |
| `simobs`, `tally` | server asks | what the simulated endpoints observed and what the outpost tallied sending ([simulation](/architecture/simulation/#what-an-endpoint-observed)) |

An outpost may also subscribe to its own reply namespace, `_INBOX.<outpost>`, and send one reply to
each request it receives. The grant is derived from the outpost's identity alone: it is a coarse
transport fence, not a second copy of the access model, which stays in the
[Storage Gateway](/architecture/storage/).

Addressing is **outpost-anchored and batch-granular**: a sample has no subject of its own, and its
name is payload, not topic. A subject per sample would explode the grant grammar without adding a
fence, and the consumer needs the whole batch anyway.

The push lane, `og.v1.api.telemetry`, sits in a segment of its own rather than under
`og.v1.telemetry.*`, so no outpost's grant can reach it. It is **trusted by subject**: the server
publishes there for `POST /telemetry:push` (whose route already checked the caller's scope) and for
`POST /webhooks/{token}`, and the consumer believes the owner a batch names only because of the
subject it arrived on. A batch on an outpost's subject that names an owner is refused.

:::design[Per-tenant NATS accounts, tracked in #529]
One NATS account per tenant, the same boundary as one database per tenant
([identity and access](/architecture/identity-access/)).
:::

## The ingest paths

Three paths carry telemetry in, and they meet at one write path, so none drifts from the others'
semantics (how a sample is landed is [metrics and properties](/architecture/properties/)):

- **An outpost** publishes one `TelemetryBatch` per tick on `og.v1.telemetry.<outpost>`, carrying a
  task group for each task it ran. The server binds each group's owner from its task's endpoint; the
  outpost names no owner ([collection](/architecture/collection/)).
- **A push**: `POST /telemetry:push` lets a scoped caller name the owner, and the API publishes the
  batch on the push lane rather than writing Postgres, so a pushed sample lands exactly as a
  collected one does.
- **A webhook**: `POST /webhooks/{token}`, whose token names one endpoint, is authenticated by its
  driver's scheme and mapped by its listeners, then published on the push lane with the endpoint's
  component as owner
  ([ADR-1024](/architecture/decisions/#adr-1024-omniglass-receives-webhooks-on-the-server-and-a-webhook-token-is-a-sealed-secret)).

Raw log lines ride any of the three in the same batch and land on their own untyped lane
([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).

## Streams and consumers

There is one stream, `OG_TELEMETRY`, over the outpost telemetry subjects and the push lane. It is a
work queue: a batch is stored the moment it is published and deleted the moment it is acked, so the
disk holds only the backlog.

The stream files each outpost's batches under a partition of its name (FNV-1a modulo the lane count),
and each partition has a durable consumer of its own, `og-telemetry-worker-<p>`, holding one batch in
flight; `og-telemetry-worker` takes the push lane
([ADR-1896](/architecture/decisions/#adr-1896-ingest-lands-on-a-lane-per-partition-of-the-nodes-name-in-order-per-node)).
So one outpost's batches land in order, and different outposts' batches land side by side in no fixed
order. Order matters for a property series, whose transition guard compares each value with the
series' latest; a metric or an event lands by its own `ts`, stamped at the edge, in any order. The lane
count is `OMNIGLASS_INGEST_PARTITIONS` ([configuration](/deployment/container-image/#configuration)),
4 by default; change it with the backlog drained, since a change moves outposts between lanes (#1934).

The worklist and heartbeat are plain request-reply, so their server side is a subscription, not a
durable: one server answers them. A second server attaching the telemetry durables joins them and
splits the deliveries ([scaling](/architecture/scaling/)).

### Delivery: a batch lands once

Exactly-once outcomes come from three parts together
([ADR-0453](/architecture/decisions/#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool),
[ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)):

- **The publish waits for the stream's ack.** A publisher lets go of a batch only once the stream
  acks it. The push route answers `202` after the ack and `503` after three attempts; an outpost holds
  an unacked batch in its spool and ships it again on the next tick
  ([outposts](/architecture/outposts/#delivery-a-batch-lands-once)).
- **`Nats-Msg-Id` dedup.** Each batch carries an id (a UUIDv7) minted once and kept, with its bytes,
  across every retry. The stream remembers an id for five minutes, so a retry inside that window is
  answered with the first copy's ack and stored once.
- **Double ack over an idempotent sink.** The consumer lands every lane of a batch in **one
  transaction** whose first statement writes the batch's **receipt** (`telemetry_receipt`), then
  double-acks. A failure part way writes nothing, and a batch whose receipt exists writes nothing, so
  a redelivery after a lost ack, or a retry past the stream's window, lands each row once. Receipts
  are kept a day.

The unit that fails is the task group, not the batch: a group the gateway refuses (its owner gone,
its writer refused, a value Postgres refuses) is skipped and its siblings land under the one receipt.
A transient failure redelivers the whole batch, after a growing delay, up to five deliveries. Every
value the database could refuse is cleaned first (a NUL byte in text or jsonb becomes U+FFFD), so one
stray byte cannot lose the rows beside it.

An ack is only as durable as the stream's store. The embedded server keeps it under
`OMNIGLASS_NATS_STORE_DIR`, a temporary directory unless set; the lab and the Helm chart put it on a
persistent volume, so a batch acked before a restart lands after it
([Helm: durable state](/deployment/helm/#durable-state)).

### Accounting every item: the loss ratio

A batch landing once does not say every item in it reached the store. The **loss ratio** does, and
reads the same for every telemetry kind (metric, property, event, log) at every hop:
`loss = (in - out) / in`. Healthy is exactly 0, so reading it needs no knowledge of a kind's
internals. A kind with nothing in has no ratio, never a 0
([ADR-1641](/architecture/decisions/#adr-1641-every-telemetry-item-is-accounted-on-its-batchs-receipt-and-read-as-a-loss-ratio)).

The hops, in the order an item crosses them:

1. **Schedule** (counted in runs): an outpost's dispatcher counts each due on a task's schedule in and
   each run it makes out, per kind the run produces. A due it passes over (the last run still in
   flight) is lost as `skipped`.
2. **Outpost**: the spool counts each item it was handed in and each item the stream acked out.
3. **Ingest**: each item a landed batch carried takes exactly one outcome.
4. **Sim** (lab only, counted in requests per protocol): what the simulated endpoints settled
   against what the outpost sent
   ([ADR-1694](/architecture/decisions/#adr-1694-the-sim-hop-is-read-per-protocol-in-requests-from-the-closed-loop)).

End to end composes as `1 - prod(1 - loss)` over the hops a kind crosses.

**The ledger rides on the batch.** An outpost's spool stamps each batch with a `DeliveryLedger`: the
spool's `boot` (a UUIDv7 minted at start), a dense `seq`, `resolved` (every earlier batch of the boot
had left the spool when this one was minted), and per kind the items it carries and the items the
boot minted before it. A retry resends the same bytes, so a missing batch leaves a hole in `seq` whose
size the neighbors' offsets give, whatever order they arrived in. The spool names its own losses on
the ledger too: the runs of seqs it dropped, each with its cause (`spool-overflow`, `spool-expired`,
`bus-refused`, `self-log-overflow`, `encode-failed`), carried until a later batch is acked
([ADR-1692](/architecture/decisions/#adr-1692-the-node-hop-reads-its-losses-from-runs-the-next-batches-carry)).
A hole the outpost does not account for was shipped and never landed, and reads at ingest as
`missing`.

**The receipt is the accounting row.** The landing transaction writes the ledger and each item's
outcome on the batch's receipt, so the accounting commits exactly when the data does. The outcomes:

- **out**: `stored`; `coalesced` (an unchanged property value the transition guard skipped on
  purpose); `unbound` (an identity no binding places, which is the operator's configuration and shows
  in the conduit's
  [unbound inbox](/architecture/collection/#shared-api-collection-one-component-many-owners)).
- **lost**: the refusals (`refused-unregistered`, `refused-writer`, `refused-instance`,
  `refused-invalid`, `refused-empty`, `refused-write`), `orphaned` (its task's owner gone),
  `unidentified`, `missing`, and the causes a whole batch is given up for (`refused-owner`,
  `refused-task`, `refused-batch`, `refused-metadata`, `abandoned` after its fifth delivery).

A refusal counts as loss on purpose: counting it out would let a fleet refusing a third of its samples
read 0. The read names the largest outcome behind a loss as its **cause**. A redelivery's items are
counted as `deduplicated` beside the ratio, never inside it, so a retry storm cannot dilute real loss.

A batch given up whole still records a **Term receipt** carrying its ledger and every item under the
cause, so the loss says why; a later copy that lands replaces it
([ADR-1691](/architecture/decisions/#adr-1691-a-batch-the-server-terms-whole-reads-under-its-own-cause-on-a-term-receipt)).
An undecodable batch has no ledger to read, so its items stay `missing`.

**The push lane keeps its own ledger.** The server stamps each pushed batch as it publishes (one
`boot` per server process), and the read gives the push lane rows of its own
([ADR-1696](/architecture/decisions/#adr-1696-the-push-lanes-batches-carry-a-ledger-minted-by-the-server-and-read-as-their-own-ingest-row)).
Pushes publish one at a time, so a push waits for its turn, and one that waits past 5 seconds is
refused with `503` before anything is minted. That is the push lane's **publish** row, counted in
pushes rather than items
([ADR-1746](/architecture/decisions/#adr-1746-a-push-refused-at-the-publish-turn-is-read-as-a-publish-row-counted-in-pushes)).

A window counts the receipts minted in it, a gap belongs to the receipt below it (so adjacent windows
sum), and a boot whose gaps could still fill reads **unsettled**, with no ratio.
`omniglass lab measure` prints the table ([lab](/developer/lab/#reading-the-delivery-table)), and
[Platform health](/operator/platform-health/#where-work-is-lost) reads the same hops live.

:::design[A killed boot's unshipped tail, tracked in #1733]
An outpost's spool is memory, so an outpost that restarts with batches in it loses them, and nothing
that landed says how many items they held. The read names such a boot's unshipped tail beside the
table; counting it on the outpost hop is not built.
:::

## The pipeline, end to end

What is built, from the edge to an alarm:

```d2
direction: right
classes: {
  box: { style.border-radius: 8 }
}
outpost: "outpost\npoll · listen · parse" { class: box }
push: "push · webhook\n(server routes)" { class: box }
stream: "OG_TELEMETRY\nwork queue" { shape: queue }
lanes: "ingest lanes\nconfine · land under receipt" { class: box }
pg: "Postgres\nmetric · property · event · log_line" { shape: cylinder }
ticks: "server ticks\ncalc · rule sweep · notifications" { class: box }
outpost -> stream: "og.v1.telemetry.<outpost>"
push -> stream: "og.v1.api.telemetry"
stream -> lanes
lanes -> pg: "one transaction a batch"
pg -> ticks: "pending marks · rows"
ticks -> pg: "derived samples · alarms · deliveries"
```

The edge parses a device's answer into samples, so no raw payload crosses the bus
([collection](/architecture/collection/)). The ingest lanes confine each group to the outpost's own
tasks and land it; the landing transaction evaluates the event rules that read the batch's keys and
marks pending the calc rules it feeds. Everything downstream of the store runs on the server's ticks
([workers](/architecture/workers/)).

:::design[Two lanes, one bus, tracked in #2611]

## Two lanes, one bus

The target splits traffic by what is moving:

- **The data lane carries samples on the bus.** Untrusted publishers (an outpost, a webhook sender)
  publish to a raw ingress subject; an **admission consumer** owner-confines each sample and
  republishes the confined ones, original `Nats-Msg-Id` kept, to a **trusted** stream. Trusted server
  producers (calc output, a command's intended write) publish to the trusted stream directly. The rule
  engine reads the trusted stream live, and a **persistence consumer** sinks it into the sample
  tables asynchronously, so a slow sink holds up only the durable copy, never the live signal.
  Confinement happens before evaluation: a forged owner is dropped before it can open an alarm.
- **The record lane is Postgres first, then CDC.** Events, alarms, actions and operator writes are
  born in a Postgres transaction. A leader-elected **CDC publisher** reads committed changes off the
  WAL through a replication slot (ensured in the boot phase, never a migration) and publishes each to
  JetStream for the action, reconcile and projection consumers, at least once with an idempotency key
  per change. No dual write and no `LISTEN`/`NOTIFY` fan-out.

Today one stream's ingest lanes do admission and persistence inline, and the record lane's work runs
on ticks over Postgres rows ([ADR-0618](/architecture/decisions/#adr-0618-notifications-are-postgres-rows-drained-by-a-server-tick-not-cdc-to-a-jetstream-sender)).
:::

## Measuring the pipeline

The server measures its own pipeline and lands the figures on its platform component once a report
window (`collection.report_seconds`), beside its request timing
([self-monitoring](/architecture/self-monitoring/#self-monitoring-the-servers-account-of-itself)). Every
failure count sits beside its attempts, so a loss reads as a ratio where 0 is healthy.

- **`ingest-lag`, `ingest-lag-max`**: the server's clock at the batch's commit minus the sample's
  `ts`, so it covers the whole trip from the outpost's spool to the landing. `ingest-lag` is the
  window's mean over each batch's newest and oldest sample, `ingest-lag-max` its worst. Healthy is a
  few seconds. The seeded `ingest-lag-high` rule opens a warning alarm when `ingest-lag-max` holds
  over 30 s for 2 minutes. Lag subtracts two clocks, so an outpost clock ahead of the server's reads
  short, even negative; a negative lag is kept as the evidence of skew
  ([ADR-1628](/architecture/decisions/#adr-1628-ingest-lag-is-the-commit-time-minus-the-sample-ts-read-on-the-server)).
- **`ingest-batch-count`, `ingest-sample-count`**: the batches committed and the samples they
  landed. A redelivery its receipt held to a no-op adds nothing.
- **`delivery-in-count`, `delivery-out-count`, `delivery-loss-ratio`**, per item kind: the ingest
  hop's loss over the window, the same three keys an outpost writes for its own hop
  ([self-monitoring](/architecture/self-monitoring/#self-monitoring-every-outposts-account-of-itself)). All three land
  every window, the ratio 0 when nothing came in.
- **`ingest-lane-busy`**: the share of the window the busiest lane spent landing. Near 1 with a
  growing backlog, the lane is the ceiling; low with a high lag, the lag is upstream of it.
- **`bus-consumer-pending`, `bus-consumer-ack-pending`, `bus-consumer-redelivered`**: the lanes'
  durables at the window's close, summed. A pending count that grows window over window is a backlog
  the lanes are not clearing.
- **`bus-stream-last-seq`, `bus-consumer-ack-floor`**: the newest batch the stream took and the
  sequence at and below which every batch was answered. The gap is the backlog in sequences.
- **`bus-telemetry-ack-failure-count`**: deliveries whose double ack was not confirmed. The receipt
  holds the redelivery to a no-op, so this costs work, not rows.
- **`bus-worklist-pull-count`**, per outcome (`served`, `read-failed`, `oversize`, `respond-failed`,
  `expired`, `abandoned`, `busy`), and **`bus-worklist-reply-bytes-max`**: every outcome but
  `served` is a loss, and a reply nearing the bus payload limit is the
  warning before `oversize`.
- **`bus-request-count`, `bus-request-failure-count`**, per path (`heartbeat`, `command-pull`,
  `command-report`).
- **`push-lane-publish-count`, `push-lane-refusal-count`, `push-lane-turn-wait-p99`,
  `push-lane-turn-wait-max`**: the push lane's publish turn.

A count reads its window's sum, then one zero after its last activity, then nothing; a gauge reads
nothing in a window with no observation, so a missing figure means no activity. What the figures say
about scaling is on [scaling](/architecture/scaling/#scale-up-or-scale-out-what-the-figures-say); the
calc, notification and sweep figures are on [workers](/architecture/workers/#measuring-the-ticks).

## The live console

The console follows live data over HTTP, never the bus, so the live path has no authorization model
of its own. `GET /views:watch` is one Server-Sent Events stream per page that carries every binding's
new rows from the ingest landing point, each binding resuming from the cursor its `POST /views:run`
history was read at, with no gap and no duplicate at the seam. It is gated and scoped as the history
read is, and bounded in lifetime so its scope never goes stale
([ADR-1830](/architecture/decisions/#adr-1830-a-view-binding-reads-its-history-and-follows-the-tail-from-the-same-cursor),
[views](/architecture/views/)).

:::design[Derived rows and record changes on the live stream, tracked in #1830]
The tail carries what the ingest lanes land. Rows a calc or event rule derives, and record changes
such as an alarm's transition, are not on it yet.
:::

:::design[The JetStream KV and object store, tracked in #529]
**KV** for config, distributed locks and leader election, and the user and permission cache;
**object store** for internal artifacts. User files stay on the
[blob store](/architecture/files/).
:::
