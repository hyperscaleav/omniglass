---
title: Workers
description: One worker machinery over several worklists, plus the backtest capability and the reconcile desired-state loop.
sidebar:
  badge:
    text: Spec
    variant: caution
---

Leaf of the [architecture spine](/architecture/). How background processing is structured: one
worker machinery, several worklists, no bespoke loops.

## One machinery, several worklists

There is one worker machinery, a **`SKIP LOCKED` worklist drain** over a configurable concurrency
pool (claim, do work, mark, all in one transaction so it inherits crash recovery, exactly-once, and
event-time semantics for free). It is instantiated over several worklists rather than separate
loops:

- **the rule engine** (the work queue): drains arriving datapoints, applies `calc_rule`s and
  `event_rule`s, and writes derived datapoints, events, and alarm transitions;
- **the outbox relay** ([alarms and actions](/architecture/alarms-actions/)): drains the action
  `outbox`, sends at-least-once, advances action step state;
- **the clock worker** ([time](/architecture/time/)): drains the `timer` table, fires schedules and
  armed timers;
- **reconcile**: the desired-state loop (below).

Each worklist is the "produces new events, needs independent durability" exception applied: a
subsystem that consumes the same event is **a stage, not a second loop**. Alongside the drains, a
**node-liveness sweep** runs on its own ticker. Unlike a worklist it is a *poll*, not a drain: a
down node produces no row to claim, so it is found by scanning heartbeat freshness, raising and
resolving the node-owned `node.down` alarm idempotently (the one-open index). There is no separate
projector either: current state is **views by default** ([storage](/architecture/storage/)), and
`alarm` / `action` hold their state directly.

## Re-entry, not one mega-pass

The pipeline `datapoint -> alarm -> action` is **not one transaction**. A datapoint arrives;
`event_rule`s evaluate it (the stateless then stateful stages below); two edges re-enter:
**calc** (a `calc_rule` produces *new* datapoints) re-enters via the `calc_work` worklist, and
**actions** enqueue to the `outbox` drained by the relay. So the rule engine never recurses
unboundedly in one transaction; a cross-producing stage hands off to a worklist, which is also what
makes it independently durable and ordered. Calc re-entry **terminates by write-on-change** (a
recompute that lands the same value enqueues nothing, the fixpoint) with a depth cap as a
cyclic-rule backstop, carrying a rollup (component -> system -> location health) one hop per drain
pass. Parsing into datapoints is **not** a worker stage; it happens at the edge
([collection](/architecture/collection/)).

## The stateless / stateful fork

This is the axis that decides almost everything else about a subsystem.

- **Stateless** (owner resolution, calc): output is a pure function of (input, rules, snapshot).
  Order-free, replayable for free, no cross-event state. Write pattern: **append** (a batched
  multi-row INSERT).
- **Stateful** (the alarm lifecycle): maintains persisted state across events (the open alarm), so
  open and resolve depend on prior state. Consequences:
  - **Order-sensitive.** The parallel claim can reorder same-key events, so a stateful subsystem
    must either be idempotent and tolerate reorder (an as-of conflict rule) or partition its
    worklist by state key.
  - Write pattern: **guarded conditional upsert** (`INSERT ... ON CONFLICT` / `UPDATE ... WHERE`),
    with a **partial unique index** as the concurrency-correctness backstop.
  - **Replay is harder**: it must process each entity's series in order.

## Lineage the engine stamps

Every derived datapoint carries its lineage **on the row** (a `provenance`, `source_rule` plus
version, and the one provenance pointer; see [storage](/architecture/storage/),
[datapoints](/architecture/datapoints/)). There is no separate execution table: a derived row is itself
the evidence of its rule's run, and a fan-out (one execution to N datapoints) stamps the same
`source_rule` on each. The rule version is the hinge for backtest.

## Backtest: re-run a changed rule over retained datapoints

The model is **not event-sourced**: current state lives in the datapoint tables and the `alarm` /
`action` rows directly, not reconstructed by replaying a log. But a changed `calc_rule` or
`event_rule` can be **backtested**: re-run the new rule version over the **retained datapoints** and
diff its output against what the old version produced, without touching live state. Only the
**calculated** and **event-derived** slices are server-rule-derived, so only they re-derive.
Everything else does not:

- **observed** datapoints are parsed at the edge and are not re-derived server-side (the raw payload
  is not stored, so there is no server-side re-parse);
- **operator alarm transitions** (ack, snooze) come from `audit_log`;
- **action delivery status** comes from the outbox (the real-world send is not re-done);
- **no-data staleness** re-derives from the datapoint gaps ([time](/architecture/time/)).

Two modes, switched by the `source_rule` version: **historical** uses the original rule versions
recorded on each derived row (reconstructing what the system actually computed, for audit), and
**prospective** uses the current rule versions (re-deriving as if today's rules had always applied,
for testing a rule change). **Replay writes to a shadow, never live**: promoting a result to live is
a separate, explicit, audited step. Prospective replay is **windowed by default** (over the last 30
days), with whole-history the explicit, heavier option.

## Reconcile: the desired-state control loop

Reconcile is another worklist consumer: it projects **declared desired state** onto the things that
drift, the system-level form of [config](/architecture/variables/)'s `reconcile: enforce`
policy.

- **Inputs**: the desired declarations (templates, component assignments, config
  declared values) plus the observed state. Config changes arrive as `audit_log` rows
  ([audit](/architecture/audit/)), so reconcile is an audit-log consumer plus the current
  projections.
- **Output**: it asserts the delta as **node config** (which tasks and commands each node runs,
  derived from placements) and as **reconciled `run` actions** (the desired-state commands that must
  stay asserted, for example a codec's feedback registration).
- **Idempotent**: assert-equals-observed is a no-op; it acts only on drift. Its runs log an
  `internal_log`, using the same worker machinery without a bespoke loop.

Open: the reconcile cadence (continuous versus on-audit-change versus a periodic full sweep) and
backoff on a flapping target.
