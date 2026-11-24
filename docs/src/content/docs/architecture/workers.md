---
title: Workers
description: "How the server does its background work: the telemetry consumer, the ticks that drain Postgres rows, how each reports keeping up, and the consumer machinery, backtest and reconcile still to come."
sidebar:
  order: 35
  badge:
    text: Partial
    variant: note
---

The server's background work is everything that happens without a request waiting on it: landing
telemetry, computing calc rules, sending notifications, settling commands, holding event rule streaks
for their sustain, and pruning what has aged out. Two shapes carry it today. Telemetry arrives on the
bus, so a **JetStream consumer** lands it. Everything else is born as a Postgres row, so a **server
tick** drains it: a loop on a fixed interval. The ticks that claim work rows (calc marks,
notifications, snoozes) claim them with `FOR UPDATE SKIP LOCKED`, so two servers against one
database share the work, and a restart resumes where the last pass stopped.

## One machinery, several consumers

The telemetry consumer is the one consumer built
([messaging](/architecture/messaging/#delivery-a-batch-lands-once)). It carries the exactly-once
triple every consumer here is meant to share: the stream deduplicates a batch's `Nats-Msg-Id`, the
consumer double-acks, and the sink is idempotent, a batch's receipt written in the batch's own
transaction
([ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).
It runs as one durable per partition of the outpost's name, each holding one batch in flight, so an
outpost's batches land in order while the partitions land side by side
([ADR-1896](/architecture/decisions/#adr-1896-ingest-lands-on-a-lane-per-partition-of-the-nodes-name-in-order-per-node)).
Order matters for the property lane, whose transition guard compares a value with the series' latest:
a lock per series stops two batches writing one value twice, but cannot reorder them. A transient
failure is redelivered after a growing delay, up to five deliveries; the consumer then gives the batch
up and records why ([accounting](/architecture/messaging/#accounting-every-item-the-loss-ratio)).

The rest runs on ticks, each a goroutine of its own in `internal/bus` or `internal/notifier`, so a slow
pass of one never delays another:

| Tick | Every | What it drains |
| --- | --- | --- |
| calc | 1 s | up to 500 pending calc marks a pass, each in its own transaction, oldest first ([ADR-0148](/architecture/decisions/#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer)) |
| notification | 2 s | the queued `notification` rows an alarm transition wrote, and the snoozes past their deadline ([ADR-0618](/architecture/decisions/#adr-0618-notifications-are-postgres-rows-drained-by-a-server-tick-not-cdc-to-a-jetstream-sender), [ADR-1318](/architecture/decisions/#adr-1318-a-snooze-is-a-fact-about-an-alarms-condition-its-notifications-are-written-suppressed-and-its-end-tells-people-again)) |
| settlement | 5 s | commands past their settle window, then event rule streaks held for their sustain ([ADR-0144](/architecture/decisions/#adr-0144-alarms-are-raised-by-event-rules-at-a-severity-level-from-a-ranked-registry)) |
| rule reconcile | 1 min | event rule state against the newest samples, healing a rare race; once at start too |
| check-in age | 15 s | every claimed outpost's `checkin-age` |
| orphan flush | 1 min | the orphaned-batch counts on each outpost row |
| receipt prune | 1 h | telemetry receipts older than a day |
| sample retention | 1 h | samples past their retention window ([storage](/architecture/storage/#what-a-retention-pass-reports)) |

A silent outpost produces no message, so its liveness is read rather than drained: the check-in age
timer writes the seconds since each outpost last spoke, and the seeded `outpost-silent` rule reads that
series like any other
([ADR-1707](/architecture/decisions/#adr-1707-a-silent-node-alarms-through-a-seeded-rule-over-its-checkin-age-not-a-node-liveness-sweep)).

There is no projector stage: a current value is read from the latest series row
([storage](/architecture/storage/)), and `alarm` holds its own state. Two pieces of work are
deliberately **not** background work. [Health](/architecture/health/) rolls up inside the transaction
that changed it, since a rollup a hop later would record its transition at the wrong moment. Parsing
happens at the edge ([collection](/architecture/collection/)).

:::design[One firing engine for the ticks, tracked in #419]
Each tick is an independent loop. The clock is to be one firing engine for all of them, with a
singleton lease and a missed-fire policy per register (code-declared jobs, schedule rows, armed
timers), so new periodic work stops arriving as another ticker. It is also what fires schedules and
the `origin=scheduled` events ([time](/architecture/time/)).
:::

:::design[Consumers for derivation, actions and persistence, tracked in #2611]
The target replaces the inline ingest and the Postgres-row ticks with stages on the bus, one
machinery over a pool of competing consumers (each message to exactly one member, no leader):

- an **admission consumer** that owner-confines raw ingress and republishes to a trusted stream
  ([messaging](/architecture/messaging/#two-lanes-one-bus));
- a **rule engine** that reads the trusted stream, applies calc and event rules, publishes derived
  samples back onto it, and writes events and alarm transitions to Postgres in one transaction;
- a **persistence consumer** that sinks the trusted stream into the sample tables, so rules never
  wait on Postgres;
- an **action sender** fed by a CDC publisher reading committed changes off the WAL, which takes over
  the notification tick's rows.

The CDC publisher and the clock each run as exactly one active instance. The pipeline from sample to
alarm to action is never one transaction: calc re-enters the data lane and terminates by
write-on-change (a recompute that lands the same value publishes nothing), with a depth cap as the
backstop for a cyclic rule.
:::

## Stateless and stateful work

The axis that decides how a piece of work is written:

- **Stateless** (owner resolution, calc): the output is a function of the input, the rules and a
  snapshot. Order does not matter, and the write is an append.
- **Stateful** (the alarm lifecycle): state persists across inputs, so order matters. The alarm
  transition serializes per alarm identity, the raiser's `dedup_key`
  ([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key)),
  and the write is a guarded upsert with a partial unique index as the backstop: `alarm_open_identity_key`
  allows one open alarm per component and `dedup_key`.

Every derived sample carries its lineage on the row: `provenance` says where it came from (observed,
calculated, intended or declared), and a calculated row names its `source_rule` and the rule's id, so its
history outlives the rule ([metrics and properties](/architecture/properties/)).

## Measuring the ticks

The consumer and the ticks report how they keep up on the server's own platform component once a
report window (#1628), beside the pipeline figures
([messaging](/architecture/messaging/#measuring-the-pipeline)). Every failure count sits beside its
attempts, so a loss reads as a ratio where 0 is healthy.

- **Calc**: `calc-pending-depth` is the deepest the pending set stood at any pass in the window,
  marks still waiting out their debounce included; `calc-served-count` the marks the window's passes
  computed; `calc-pass-duration` a pass's mean milliseconds. Healthy is a depth that falls back toward
  0 between bursts and a pass well under its second.
- **Notifications**: `outbox-queued-depth` is the rows still queued at the window's close, due or
  waiting out a retry backoff; `outbox-due-age` how long the oldest due row has waited past its send
  time, 0 when none is due. A failing webhook holds the depth up while the due age stays near 0, since
  a backoff is not a wait; a due age that grows says the sender is not reaching its rows.
- **Settlement**: `sweep-duration`, `sweep-count` and `sweep-failure-count`, each at two instances,
  `command-settlement` and `event-rule`. A pass that succeeds writes its failure count as 0, so
  [Platform health](/operator/platform-health/) reads each sweep's failures against its passes.

These read the server's clock against its own database, so unlike ingest lag no outpost clock is in
them ([ADR-1628](/architecture/decisions/#adr-1628-ingest-lag-is-the-commit-time-minus-the-sample-ts-read-on-the-server)).
The rules that alarm on the server itself, `rule-sweep-stalled` among them, are on
[self-monitoring](/architecture/self-monitoring/#self-monitoring-the-servers-account-of-itself).

:::design[Backtest and reconcile, tracked in #526]
**Backtest** re-runs a changed calc or event rule over the retained samples and diffs it against the
old version's output, writing to a shadow and never to live state; promoting the result is a
separate, audited step. Only calculated and event-derived data re-derives: observed samples were
parsed at the edge and no raw payload is stored. It runs historical (each row's own rule version)
or prospective (today's rules, the last 30 days by default).

**Reconcile** is the system-level form of a [declared value](/architecture/variables/)'s
`reconcile: enforce`: a loop that compares what is declared with what is observed and acts only on
drift, asserting outpost config and the commands that should stay asserted. Its cadence, and its
backoff on a flapping target, are open.
:::
