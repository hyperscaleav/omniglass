---
title: Workers
description: "One worker machinery over several JetStream consumers, plus the backtest capability and the reconcile desired-state loop."
sidebar:
  badge:
    text: Design
    variant: caution
---

Workers are how Omniglass does the steady background work, deriving datapoints, sending actions, firing timers, reconciling drift, on one machinery instead of a pile of bespoke loops, so the operator gets crash recovery and exactly-once outcomes for free everywhere.

## One machinery, several consumers

There is one worker machinery, a **JetStream work-queue consumer** over a configurable concurrency
pool (pull a message, do work, ack, with at-least-once delivery plus `Nats-Msg-Id` dedup and an
idempotent sink so it inherits crash recovery, exactly-once outcomes, and event-time semantics for
free). It is instantiated over several consumers rather than separate loops:

- **the admission consumer**: owner-confines raw-ingress datapoints (node and webhook) against the
  publisher's placement, preserving `Nats-Msg-Id`, and republishes to the **trusted** datapoints stream,
  so the rule engine and persistence read only confined points (system mode, [messaging](/architecture/messaging/));
- **the rule engine** (datapoint consumers): consume arriving datapoints from the **trusted**
  JetStream datapoints stream, apply `calc_rule`s and `event_rule`s, publish derived datapoints back
  onto the trusted stream (a trusted producer, no admission pass), and write events and alarm transitions
  to Postgres in one transaction;
- **the action sender** ([alarms and actions](/architecture/alarms-actions/)): consumes
  action work fanned out by CDC, sends at-least-once, advances action step state (PG-first, CDC-out);
- **the persistence consumer**: a batch sink that consumes the **trusted** datapoints stream and writes
  datapoints to the Postgres metric/state/log tables asynchronously, so rules never wait on PG;
- **the clock** ([time](/architecture/time/)): fires schedules and armed timers (a leader-elected
  singleton, below);
- **reconcile**: the desired-state loop (below).

Each consumer is the "produces new work, needs independent durability" exception applied: a
subsystem that consumes the same message is **a stage, not a second loop**. Competing consumers in a
group scale horizontally with no leader: JetStream hands each message to exactly one member, and
adding instances just adds throughput. Alongside the consumers, a **node-liveness sweep** runs on its
own ticker. Unlike a consumer it is a *poll*, not a drain: a down node produces no message, so it is
found by scanning heartbeat freshness, raising and resolving the node-owned `node.down` alarm
idempotently (the one-open index). There is no separate projector either: current state is **views by
default** ([storage](/architecture/storage/)), and `alarm` / `action` hold their state directly.

## Consumer groups versus singletons

Most of the machinery is competing consumers, but two pieces must run as exactly one active instance:
the **CDC publisher** (logical decoding of the WAL, fanning committed events, alarms, actions, and
operator mutations out to JetStream) and the **clock** (firing schedules and armed timers). These are
**leader-elected singletons** via a **NATS KV CAS lock**: each candidate races to compare-and-set a
KV key, the winner holds the lease, and on its death the lease expires and another candidate takes
over. Same pattern for both, no separate election service and no SKIP-LOCKED row claim. A singleton
that produces work still publishes onto the bus, where the competing consumers scale it out.

## Re-entry, not one mega-pass

The pipeline `datapoint -> alarm -> action` is **not one transaction**. A datapoint arrives on the
datapoints stream; `event_rule`s evaluate it (the stateless then stateful stages below); two edges
re-enter: **calc** (a `calc_rule` produces *new* datapoints) re-enters by publishing the derived
datapoints back onto the data lane, where the consumers pick them up again, and **actions** are born
when an `event_rule` writes the event and alarm to PG in one transaction, after which CDC fans the
committed change out to the action sender. So the rule engine never recurses unboundedly in one
transaction; a cross-producing stage hands off to the bus, which is also what makes it independently
durable. Calc re-entry **terminates by write-on-change** (a recompute that lands the same value
publishes nothing, the fixpoint) with a depth cap as a cyclic-rule backstop, carrying a rollup
(component -> system -> location health) one hop per pass. Parsing into datapoints is **not** a
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
    key. The alarm transition is serialized per `(event_rule, owner)`: that ordered write lands in
    the same PG transaction as the event record.
  - Write pattern: **guarded conditional upsert** (`INSERT ... ON CONFLICT` / `UPDATE ... WHERE`),
    with a **partial unique index** as the concurrency-correctness backstop.
  - **Backtest is harder**: it must process each entity's series in order.

## Lineage the engine stamps

Every derived datapoint carries its lineage **on the row** (a `provenance`, `source_rule` plus
version, and the one provenance pointer; see [storage](/architecture/storage/),
[datapoints](/architecture/datapoints/)). There is no separate execution table: a derived row is itself
the evidence of its rule's run, and a fan-out (one execution to N datapoints) stamps the same
`source_rule` on each. The rule version is the hinge for backtest.

## Backtest: re-run a changed rule over retained datapoints

The model is **not event-sourced**: current state lives in the datapoint tables and the `alarm` /
`action` rows directly, never reconstructed from a log. Omniglass does **not** re-run history to rebuild
events or state. But a changed `calc_rule` or `event_rule` can be **backtested**: a read-only
what-if that re-runs the new rule version over the **retained datapoints** and diffs its output
against what the old version produced, purely as DX sugar, without writing a new event or touching
live state. Only the **calculated** and **event-derived** slices are server-rule-derived, so only
they re-derive. Everything else does not:

- **observed** datapoints are parsed at the edge and are not re-derived server-side (the raw payload
  is not stored, so there is no server-side re-parse);
- **operator alarm transitions** (ack, snooze) come from `audit_log`;
- **action delivery status** comes from the action rows (the real-world send is not re-done);
- **no-data staleness** re-derives from the datapoint gaps ([time](/architecture/time/)).

Two modes, switched by the `source_rule` version: **historical** uses the original rule versions
recorded on each derived row (showing what the system actually computed, for audit), and
**prospective** uses the current rule versions (re-deriving as if today's rules had always applied,
for testing a rule change). **A backtest writes to a shadow, never live**: promoting a result to live is
a separate, explicit, audited step. A prospective backtest is **windowed by default** (over the last 30
days), with whole-history the explicit, heavier option.

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
