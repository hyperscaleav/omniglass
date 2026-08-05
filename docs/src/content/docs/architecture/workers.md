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
which does owner confinement and persistence inline.
:::

Workers are how Omniglass does the steady background work (deriving samples, sending actions, firing timers, reconciling drift) on one machinery instead of a pile of bespoke loops: crash recovery and exactly-once outcomes for free everywhere.

## One machinery, several consumers

The one machinery is a **JetStream work-queue consumer** over a configurable concurrency pool: pull, work, ack, at-least-once plus `Nats-Msg-Id` dedup and an idempotent sink (crash recovery, exactly-once outcomes, event-time semantics for free). That triple is the target contract, not the current one: today there is no `Nats-Msg-Id` dedup and the sink is not idempotent (#311, #430), and the built consumer dispatches serially on purpose, the state transition guard depending on it (`internal/bus/consumer.go:246-253`). It instantiates over several consumers rather than separate loops:

:::design[Target design, tracked in #430; the clock is #419]
- **the admission consumer**: the owner-confinement gate at the head of the data lane, running in
  system mode ([messaging](/architecture/messaging/#two-lanes-one-bus));
- **the rule engine** (sample consumers): consumes the **trusted** JetStream samples stream,
  applies `calc_rule`s and `event_rule`s, publishes derived samples back onto it (a trusted
  producer, no admission pass), and writes events and alarm transitions to Postgres in one
  transaction;
- **the action sender** ([alarms and actions](/architecture/alarms-actions/)): consumes action work
  fanned out by CDC, sends at-least-once, advances action step state (PG-first, CDC-out);
- **the persistence consumer**: the data lane's batch sink into the Postgres sample tables, async so
  rules never wait on PG ([messaging](/architecture/messaging/#two-lanes-one-bus));
- **the clock** ([time](/architecture/time/)): fires schedules and armed timers (a leader-elected
  singleton, below);
- **reconcile**: the desired-state loop (below).

A subsystem that consumes the same message is **a stage, not a second loop**; competing consumers
scale horizontally with no leader (JetStream hands each message to exactly one member).
:::

:::design[The node-liveness sweep, tracked in #419]
Alongside the consumers, a **node-liveness sweep** runs on its own ticker: a *poll*, not a drain (a
down node produces no message), scanning heartbeat freshness and raising / resolving the node-owned
`node-down` alarm idempotently over the one-open dedup primitive
([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key),
built; nodes already publish the heartbeat the sweep will read, the sweep itself is not).
:::

There is no separate projector: current values live in the `property` latest-value cache table (ADR-0065; [storage](/architecture/storage/)), and `alarm` / `action` hold their state directly.

:::design[Target design, tracked in #430 (the CDC publisher) and #419 (the clock)]

## Consumer groups versus singletons

Two pieces run as exactly one active instance: the **CDC publisher**
([messaging](/architecture/messaging/#two-lanes-one-bus)) and the **clock**. Both are
**leader-elected singletons** via a **NATS KV CAS lock** (the winner holds the lease; on its death
another candidate takes over): no separate election service, no SKIP-LOCKED row claim. A singleton
that produces work still publishes onto the bus, where competing consumers scale it out.

:::

:::design[Target design, tracked in #430]

## Re-entry, not one mega-pass

The pipeline `sample -> alarm -> action` is **not one transaction**. Two edges re-enter: **calc**
publishes derived samples back onto the data lane, and **actions** are born when an `event_rule`
writes the event and alarm to PG in one transaction, after which CDC fans the commit to the action
sender. A cross-producing stage hands off to the bus (independently durable), so the rule engine
never recurses unboundedly in one transaction; calc re-entry **terminates by write-on-change** (a
recompute landing the same value publishes nothing, the fixpoint) with a depth cap as a cyclic-rule
backstop, carrying a rollup one hop per pass.

:::

**[Health](/architecture/health/) is the exception, deliberately not a worker stage**: its rollup (component -> system -> location) runs **inside the write transaction** that changed it, because a verdict recomputed a hop later would record its transition at the wrong moment. Parsing into samples is **not** a worker stage; it happens at the edge ([collection](/architecture/collection/)).

## The stateless / stateful fork

The axis that decides almost everything else about a subsystem:

- **Stateless** (owner resolution, calc): output is a pure function of (input, rules, snapshot). Order-free, safe to backtest, no cross-event state. Write pattern: **append** (a batched multi-row INSERT).
- **Stateful** (the alarm lifecycle): maintains persisted state across events (the open alarm), so:
  - **Order-sensitive.** JetStream does not promise strict ordering (the server is ts-authoritative) and competing consumers can hand same-key messages to different members, so a stateful subsystem either tolerates reorder idempotently or serializes per state key. The alarm transition serializes per condition, today the raiser-supplied `dedup_key` ([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key)); the `event_rule` keying joins alongside when the rule engine lands, its ordered write in the same PG transaction as the event record.
  - Write pattern: **guarded conditional upsert** (`INSERT ... ON CONFLICT` / `UPDATE ... WHERE`), with a **partial unique index** as the concurrency-correctness backstop (built for alarms: `alarm_open_condition_key`, one open row per `(component, dedup_key)`).
  - **Backtest is harder**: it must process each entity's series in order.

:::design[Target design, tracked in #430]

## Lineage the engine stamps

Every derived sample carries its lineage **on the row** (`provenance`, `source_rule` plus version,
the one provenance pointer; [storage](/architecture/storage/), [samples](/architecture/properties/)).
No separate execution table: a fan-out (one execution to N samples) stamps the same `source_rule` on
each, and the rule version is the hinge for backtest.

:::

:::design[The backtest engine, tracked in #526]

## Backtest: re-run a changed rule over retained samples

The model is **not event-sourced**: current state lives in the sample tables and the `alarm` /
`action` rows, never reconstructed from a log. But a changed `calc_rule` or `event_rule` can be
**backtested**: a read-only what-if re-running the new rule version over the **retained samples**
and diffing against the old version's output, touching no live state. Only the **calculated** and
**event-derived** slices re-derive: **observed** samples are parsed at the edge (no raw stored, no
server-side re-parse), **operator alarm transitions** come from `audit_log`, **action delivery
status** from the action rows (the send is not re-done), and **no-data staleness** re-derives from
the sample gaps ([time](/architecture/time/)).

Two modes, switched by the `source_rule` version: **historical** uses the original rule versions
recorded on each derived row (what the system actually computed, for audit); **prospective** uses
the current versions (as if today's rules had always applied, for testing a change). **A backtest
writes to a shadow, never live**: promotion is a separate, explicit, audited step. A prospective
backtest is **windowed by default** (the last 30 days), whole-history the explicit, heavier option.

:::

:::design[The reconcile control loop, tracked in #526]

## Reconcile: the desired-state control loop

Reconcile is another JetStream consumer: it projects **declared desired state** onto the things that
drift, the system-level form of [config](/architecture/variables/)'s `reconcile: enforce` policy.

- **Inputs**: the desired declarations (templates, component assignments, config declared values)
  plus observed state; config changes commit in PG and CDC publishes them
  ([audit](/architecture/audit/)), so reconcile is a CDC consumer plus the current projections.
- **Output**: the delta, asserted as **node config** (which tasks and commands each node runs,
  derived from placements) and as **reconciled `run` actions** (desired-state commands that stay
  asserted, e.g. a codec's feedback registration).
- **Idempotent**: assert-equals-observed is a no-op; it acts only on drift, logging an
  `internal_log` per run.

Open: the reconcile cadence (continuous versus on-audit-change versus a periodic full sweep) and
backoff on a flapping target.

:::
