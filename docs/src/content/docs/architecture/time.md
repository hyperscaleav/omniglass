---
title: Time
description: "The one primitive that manufactures events from the passage of time, so the rest of the pipeline stays purely event-driven."
sidebar:
  badge:
    text: Design
    variant: caution
---

::::design[Target design: the time primitive, tracked in #419]

Everything else in the pipeline is **push-driven**; time is the one input that **arrives as
nothing**: "10 minutes elapsed," "it is 8am Monday," and especially "the data *stopped*" produce no
inbound event, so nothing would ever fire on them. The time primitive turns the passage of time
into events the normal pipeline consumes.

## The pair: schedule, timer

- **`schedule`** (config): a recurring definition, a cron or rrule plus an IANA timezone and what
  it triggers.

:::caution[Open question]
The recurrence surface a `schedule` accepts: a full iCalendar rrule, or a cron subset plus calendar
anchors like month-start and month-end.
:::
- **`timer`** (mechanism, working-set): every *pending* fire, kind-discriminated
  (`schedule-tick | for-sustain | runbook-wait | watchdog`), with a `fire_at` and a pointer to
  what it is for. A PG row: a mutable working set, like the outbox, **not** a history log.

## One mechanism, three patterns

All time behavior is the one `timer` table scanned by the clock singleton (sorted by `fire_at`,
woken by a ticker with a crash-recovery backstop), each due row's fire realized on its lane (see
[Storage](#storage)):

- **recurring** (a schedule): reschedule the next `fire_at` after firing. Digests, synthetic
  checks, SLA calendar resets.
- **armed and cancellable** (a relative one-shot): armed by an event, fires later, cancelled if
  the condition clears. The `for`-duration sustain, runbook waits, escalation delays.
- **reset-on-arrival** (a watchdog): pushed to `now + tolerance` on each sample, fires if it
  lapses. No-data and staleness.

Single-fire across replicas: the clock is a leader-elected singleton (a NATS KV CAS lock, failover
on death), so no replica races another to claim a row.

:::caution[Open question]
Whether a runbook's per-step waits each get their own `timer` row, or one row is advanced per step.
:::

:::caution[Open question]
The clock singleton's wake strategy: wake-on-insert for near-term fires plus a coarse backstop
ticker, so a far-future schedule needs no frequent ticks.
:::

## A fire is recorded once, on the log of what it produces

The `timer` table is mechanism; the **event is the product**. Each fire lands on the log of
whatever it drives, never twice:

| Timer kind | Produces | Logged on |
|---|---|---|
| schedule-tick | a trigger | an `event` (`origin=scheduled`) |
| for-sustain | the alarm opens | an `event` (alarm edge) |
| runbook-wait | the action advances | the `action` row |
| watchdog | the property goes stale | `metric` / `property` (a derived staleness sample) |

The high-churn watchdog never floods an event log with its resets. A schedule fire is an ordinary
**`event` with `origin=scheduled`**, manufactured by the clock into the `event` log in a PG
transaction like any other event (never published directly, no dual-write); an `action_rule`
consumer reacts to it like any other event, the uniform "rules consume events" model:

```yaml
action_rule:
  on: event
  when: 'origin == "scheduled" && schedule == "daily-digest"'
  action: email-open-alarms-summary
```

A synthetic check, an SLA window reset, and a digest are all schedules whose fire an action
subscribes to; a digest's aggregating action queries (open alarms, the day's events), renders a
Go-template body ([alarms and actions](/architecture/alarms-actions/)), and sends. No new
machinery.

## The backtest split

- **Schedules and armed timers are ground truth.** The wall clock genuinely advanced; a backtest
  does not re-run the clock, it reads the recorded `origin=scheduled` events as-is.
- **No-data is derived.** The gap is *already in the recorded data* (absent sample rows in a
  window), so a backtest re-detects the same gaps with no clock. Runtime needs a real watchdog,
  but logically it is a `calc_rule` reading arrival times.

## No-data: stale vs unknown

Absence of data is two conditions:

- **`stale`**: we *had* a value and it aged past its expected cadence. The watchdog's product (it
  can only arm after a first arrival); the last value and its **age are retained**. Usually
  **actionable**: a stopped signal most often means lost visibility. Emits a derived staleness
  sample (`X stale at T`, `fresh again` on resume).
- **`unknown`**: **never** observed. No baseline, no last value. A static "not monitored yet"
  condition (a fresh device, a property_type never reported), detected by "no observations
  exist," not by a watchdog. Gray, not actionable.

The built current-value shape is the `property` cache row, `CachedValue{Value, TS, Provenance}`.

:::design[The `freshness` quality on the current value, tracked in #430]
The cache row carries a `freshness (fresh | stale)` quality: staleness marked on the current
value with the last value preserved.
:::

**[Health](/architecture/health/) treats them differently**: a *stale required member* defaults to
`unknown` (lost visibility, so the system rolls to `unknown`); an *unknown member* is gray and does
not down the system. They surface as the health `unknown` reasons `stale` and `no-data` (distinct
from `uncovered`). Whether stale means "last value still valid" or "lost visibility, alarm" is
**per-property-type policy**: the `property_type` declares its staleness tolerance.

**Cadence is inferred for pollers, declared for heartbeats.** A poller's expected interval is its
`interval` times a tolerance. A listen-triggered function is **opt-in**: watched only if it
declares an expected heartbeat interval (an MQTT keepalive); undeclared silence is normal and
unwatched.

:::caution[Open question]
The watchdog tolerance defaults (the multiplier on a poller's `interval`) and whether to debounce a
missed-poll burst before declaring stale.
:::

## Timezones

Every stored instant is a **`timestamptz`** (UTC, tz-aware). A **`schedule` additionally carries an
IANA timezone** (`America/New_York`) for recurrence and calendar boundaries, because DST means
"8am" and "the 1st of the month" cannot be precomputed as fixed offsets. The resolved `fire_at` is
a `timestamptz`; the recurrence is computed in the schedule's timezone.

## Storage

The recurring trigger config and the clock singleton's pending-fire working set; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `schedule` | id, rrule/cron, **tz (IANA)**, target, enabled | config: a recurring trigger |
| `timer` | id, **fire_at (timestamptz)**, kind (schedule-tick / for-sustain / runbook-wait / watchdog), ref, payload | the clock singleton's pending-fire **working-set** (the durable PG working set, mutable, scanned for due rows and the fire realized on its lane: a record-lane fire born in PG and CDC-fanned to JetStream, a watchdog's staleness onto the data lane), not a history log; fires are logged on the entity they produce |
::::
