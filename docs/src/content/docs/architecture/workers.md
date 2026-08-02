---
title: Workers
description: "One worker machinery over several JetStream consumers, plus the backtest capability and the reconcile desired-state loop."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Implementation status]
Built today: one durable JetStream work-queue consumer, `og-telemetry-worker`
(`internal/bus/consumer.go`: explicit ack, MaxDeliver 5, bounded nak backoff, serial dispatch),
which does owner confinement and persistence inline. The design fences below mark the parts of this
page that are target design, each naming its tracker.
:::

Workers are how Omniglass does the steady background work, deriving samples, sending actions, firing timers, reconciling drift, on one machinery instead of a pile of bespoke loops, so the operator gets crash recovery and exactly-once outcomes for free everywhere.

## One machinery, several consumers

There is one worker machinery, a **JetStream work-queue consumer** over a configurable concurrency
pool (pull a message, do work, ack, with at-least-once delivery plus `Nats-Msg-Id` dedup and an
idempotent sink so it inherits crash recovery, exactly-once outcomes, and event-time semantics for
free). That triple is the target contract, not the current one: today there is no `Nats-Msg-Id`
dedup and the sink is not idempotent (#311, #430), and the one built consumer dispatches serially
on purpose because the state transition guard depends on it (`internal/bus/consumer.go:246-253`),
so the configurable concurrency pool and exactly-once outcomes are Design. It is instantiated over
several consumers rather than separate loops:

:::design[Target design, tracked in #430; the clock is #419]
- **the admission consumer**: the owner-confinement gate at the head of the data lane, a worker
  running in system mode; the mechanism is described once, on
  [messaging](/architecture/messaging/#two-lanes-one-bus);
- **the rule engine** (sample consumers): consume arriving samples from the **trusted**
  JetStream samples stream, apply `calc_rule`s and `event_rule`s, publish derived samples back
  onto the trusted stream (a trusted producer, no admission pass), and write events and alarm transitions
  to Postgres in one transaction;
- **the action sender** ([alarms and actions](/architecture/alarms-actions/)): consumes
  action work fanned out by CDC, sends at-least-once, advances action step state (PG-first, CDC-out);
- **the persistence consumer**: the data lane's batch sink into the Postgres sample tables, async so
  rules never wait on PG ([messaging](/architecture/messaging/#two-lanes-one-bus));
- **the clock** ([time](/architecture/time/)): fires schedules and armed timers (a leader-elected
  singleton, below);
- **reconcile**: the desired-state loop (below).

Each consumer is the "produces new work, needs independent durability" exception applied: a
subsystem that consumes the same message is **a stage, not a second loop**. Competing consumers in a
group scale horizontally with no leader: JetStream hands each message to exactly one member, and
adding instances just adds throughput.
:::

:::design[The node-liveness sweep, tracked in #419]
Alongside the consumers, a **node-liveness sweep** runs on its
own ticker. Unlike a consumer it is a *poll*, not a drain: a down node produces no message, so it is
found by scanning heartbeat freshness, raising and resolving the node-owned `node.down` alarm
idempotently over the one-open dedup primitive
([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key), which is built; nodes already publish the heartbeat the sweep will read, and the sweep itself is not).
:::

There is no separate projector: current values live in the
`property` latest-value cache table (ADR-0065; see [storage](/architecture/storage/)), and
`alarm` / `action` hold their state directly.

:::design[Target design, tracked in #430 (the CDC publisher) and #419 (the clock)]

## Consumer groups versus singletons

Most of the machinery is competing consumers, but two pieces must run as exactly one active instance:
the **CDC publisher** (the record lane's bridge from Postgres commits onto JetStream, described once
on [messaging](/architecture/messaging/#two-lanes-one-bus)) and the **clock** (firing schedules and armed timers). These are
**leader-elected singletons** via a **NATS KV CAS lock**: each candidate races to compare-and-set a
KV key, the winner holds the lease, and on its death the lease expires and another candidate takes
over. Same pattern for both, no separate election service and no SKIP-LOCKED row claim. A singleton
that produces work still publishes onto the bus, where the competing consumers scale it out.

:::

:::design[Target design, tracked in #430]

## Re-entry, not one mega-pass

The pipeline `sample -> alarm -> action` is **not one transaction**. A sample arrives on the
samples stream; `event_rule`s evaluate it (the stateless then stateful stages below); two edges
re-enter: **calc** (a `calc_rule` produces *new* samples) re-enters by publishing the derived
samples back onto the data lane, where the consumers pick them up again, and **actions** are born
when an `event_rule` writes the event and alarm to PG in one transaction, after which CDC fans the
committed change out to the action sender. So the rule engine never recurses unboundedly in one
transaction; a cross-producing stage hands off to the bus, which is also what makes it independently
durable. Calc re-entry **terminates by write-on-change** (a recompute that lands the same value
publishes nothing, the fixpoint) with a depth cap as a cyclic-rule backstop, carrying a rollup one hop
per pass.

:::

**[Health](/architecture/health/) is the exception, and deliberately not a worker stage**: its
rollup (component -> system -> location) runs **inside the write transaction** that changed it, because a
verdict recomputed a hop later would record its transition at the wrong moment. Parsing into samples is **not** a
worker stage; it happens at the edge ([collection](/architecture/collection/)).

## The stateless / stateful fork

This is the axis that decides almost everything else about a subsystem.

- **Stateless** (owner resolution, calc): output is a pure function of (input, rules, snapshot).
  Order-free, safe to backtest for free, no cross-event state. Write pattern: **append** (a batched
  multi-row INSERT).
- **Stateful** (the alarm lifecycle): maintains persisted state across events (the open alarm), so
  open and resolve depend on prior state. Consequences:
  - **Order-sensitive.** JetStream does not promise strict ordering (the server is ts-authoritative)
    and competing consumers can hand same-key messages to different members, so a stateful subsystem
    must either be idempotent and tolerate reorder (an as-of conflict rule) or serialize per state
    key. The alarm transition serializes per condition: today the condition identity is the
    raiser-supplied `dedup_key`
    ([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key));
    the `event_rule` keying joins alongside when the rule engine lands, and its ordered write will
    land in the same PG transaction as the event record.
  - Write pattern: **guarded conditional upsert** (`INSERT ... ON CONFLICT` / `UPDATE ... WHERE`),
    with a **partial unique index** as the concurrency-correctness backstop (built for alarms:
    `alarm_open_condition_key`, one open row per `(component, dedup_key)`).
  - **Backtest is harder**: it must process each entity's series in order.

:::design[Target design, tracked in #430]

## Lineage the engine stamps

Every derived sample carries its lineage **on the row** (a `provenance`, `source_rule` plus
version, and the one provenance pointer; see [storage](/architecture/storage/),
[samples](/architecture/properties/)). There is no separate execution table: a derived row is itself
the evidence of its rule's run, and a fan-out (one execution to N samples) stamps the same
`source_rule` on each. The rule version is the hinge for backtest.

:::

:::design[Target design, tracked in #434]

## Backtest: re-run a changed rule over retained samples

The model is **not event-sourced**: current state lives in the sample tables and the `alarm` /
`action` rows directly, never reconstructed from a log. Omniglass does **not** re-run history to rebuild
events or state. But a changed `calc_rule` or `event_rule` can be **backtested**: a read-only
what-if that re-runs the new rule version over the **retained samples** and diffs its output
against what the old version produced, purely as DX sugar, without writing a new event or touching
live state. Only the **calculated** and **event-derived** slices are server-rule-derived, so only
they re-derive. Everything else does not:

- **observed** samples are parsed at the edge and are not re-derived server-side (the raw payload
  is not stored, so there is no server-side re-parse);
- **operator alarm transitions** (ack, snooze) come from `audit_log`;
- **action delivery status** comes from the action rows (the real-world send is not re-done);
- **no-data staleness** re-derives from the sample gaps ([time](/architecture/time/)).

Two modes, switched by the `source_rule` version: **historical** uses the original rule versions
recorded on each derived row (showing what the system actually computed, for audit), and
**prospective** uses the current rule versions (re-deriving as if today's rules had always applied,
for testing a rule change). **A backtest writes to a shadow, never live**: promoting a result to live is
a separate, explicit, audited step. A prospective backtest is **windowed by default** (over the last 30
days), with whole-history the explicit, heavier option.

:::

:::design[Target design, tracked in #434]

## Reconcile: the desired-state control loop

Reconcile is another JetStream consumer: it projects **declared desired state** onto the things that
drift, the system-level form of [config](/architecture/variables/)'s `reconcile: enforce`
policy.

- **Inputs**: the desired declarations (templates, component assignments, config
  declared values) plus the observed state. Config changes are operator mutations born in a PG
  transaction; CDC publishes the committed change to JetStream
  ([audit](/architecture/audit/)), so reconcile is a CDC consumer plus the current
  projections.
- **Output**: it asserts the delta as **node config** (which tasks and commands each node runs,
  derived from placements) and as **reconciled `run` actions** (the desired-state commands that must
  stay asserted, for example a codec's feedback registration).
- **Idempotent**: assert-equals-observed is a no-op; it acts only on drift. Its runs log an
  `internal_log`, using the same worker machinery without a bespoke loop.

Open: the reconcile cadence (continuous versus on-audit-change versus a periodic full sweep) and
backoff on a flapping target.

:::
