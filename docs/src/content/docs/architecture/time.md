---
title: Time
description: "The one primitive that manufactures events from the passage of time, so the rest of the pipeline stays purely event-driven."
sidebar:
  badge:
    text: Design
    variant: caution
---

Time lets an operator alarm on things that produce no event of their own, "10 minutes elapsed", "it is 8am Monday", "the data stopped", by turning the passage of time into events the rest of the pipeline consumes.

## Why time needs a primitive

Everything else is **push-driven**: an event arrives, rules fire. Time is the one input that
**arrives as nothing**. "10 minutes elapsed," "it is 8am Monday," and especially "the data
*stopped*" produce no inbound event, so nothing would ever fire on them. This primitive's whole
job is to turn the passage of time into events the normal pipeline consumes.

## The pair: schedule, timer

- **`schedule`** (config): a recurring definition, a cron or rrule plus an IANA timezone and what
  it triggers. Config, like a rule.

:::caution[Open question]
The recurrence surface a `schedule` accepts: a full iCalendar rrule, or a cron subset plus calendar
anchors like month-start and month-end.
:::
- **`timer`** (mechanism, working-set): every *pending* fire, kind-discriminated
  (`schedule-tick | for-sustain | runbook-wait | watchdog`), with a `fire_at` and a pointer to
  what it is for. A PG row, the durable working set. The clock singleton scans due rows and
  realizes each fire on its lane (a record-lane fire is written to PG and CDC fans it out to
  JetStream; a watchdog's staleness enters the data lane as a derived datapoint); rows are then
  consumed and rescheduled. A mutable working-set, like the outbox, **not** a history log.

A schedule fire is **not** a separate log table: it is an ordinary **`event` with
`origin=scheduled`**, manufactured by the clock into the `event` log. The event is born in a PG
transaction (record plus any alarm transition) the same as any other event, never published
directly (no dual-write), and the history of schedule fires lives in the `event` log alongside
caught, caused, and derived events. The leader-elected CDC publisher fans the committed event out
to JetStream, where an `action_rule` consumer reacts to it exactly as it reacts to any other event.

## One mechanism, three patterns

All time behavior is the one `timer` table scanned by the clock singleton (sorted by `fire_at`,
woken by a ticker with a crash-recovery backstop), each due row's fire realized on its lane (a
record-lane fire born in PG and CDC-fanned to JetStream, a watchdog's staleness onto the data lane):

- **recurring** (a schedule): reschedule the next `fire_at` after firing. Digests, synthetic
  checks, SLA calendar resets.
- **armed and cancellable** (a relative one-shot): armed by an event, fires later, cancelled if
  the condition clears. The `for`-duration sustain, runbook waits, escalation delays.
- **reset-on-arrival** (a watchdog): pushed to `now + tolerance` on each datapoint, fires if it
  lapses. No-data and staleness.

Durable (a table, survives restart), single-fire across replicas: the clock is a leader-elected
singleton, exactly one active at a time, held by a NATS KV CAS lock and failed over on death, so
no replica races another to claim a row.

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
| watchdog | the datapoint goes stale | `datapoint` |

So every schedule fire is an `event` with `origin=scheduled`, and every other timer fire is on
the entity it advances. No untracked fires, no double-logging, and the high-churn watchdog never
floods an event log with its resets.

## The backtest split

Time divides cleanly across the backtest boundary:

- **Schedules and armed timers are ground truth.** The wall clock genuinely advanced and a digest
  genuinely went out at 8am; a backtest does not re-run the clock, it reads the recorded
  `origin=scheduled` events as-is.
- **No-data is derived.** The gap is *already in the recorded data* (the absence of datapoint rows
  in a window), so a backtest re-detects the same gaps and would re-emit the same staleness, no clock
  needed. At runtime it needs a real watchdog (you cannot know data is missing until the deadline
  passes), but logically it is a `calc_rule` reading arrival times.

## A schedule fire is the `origin=scheduled` event

An `action_rule` consumer reacts to a schedule fire exactly as it reacts to an alarm, so
`origin=scheduled` is the uniform "rules consume events" model, not special wiring:

```yaml
action_rule:
  on: event
  when: 'origin == "scheduled" && schedule == "daily-digest"'
  action: email-open-alarms-summary
```

A synthetic check, an SLA window reset, and a digest are all schedules whose fire an action (or a
check) subscribes to.

## No-data: stale vs unknown

Absence of data is two conditions, and the why matters:

- **`stale`**: we *had* a value and it has aged past its expected cadence. The watchdog's product
  (it can only arm after a first arrival). The last value and its **age are retained**; usually
  **actionable**, because a signal that stopped most often means lost visibility (the source
  died). The watchdog emits a derived staleness datapoint (`X stale at T`, and `fresh again` on
  resume).
- **`unknown`**: **never** observed. No baseline, no last value. A static "not monitored yet"
  condition (a fresh device, a property_type never reported), detected by "no observations
  exist," not by a watchdog. Gray, not actionable.

`current_value` carries `value, as_of_ts, freshness (fresh | stale)`; staleness is a quality of
the datapoint with the last value preserved. **[Health](/architecture/health/) treats them
differently**: a *stale required member* defaults to `unknown` (lost visibility, so the system
rolls to `unknown`, [health](/architecture/health/)), an *unknown member* is gray and does not down the system. Whether stale means "last value still valid" (a
slow config signal) or "lost visibility, alarm" (a liveness signal) is **per-datapoint-type
policy**: the property_type declares its staleness tolerance.

These two absences surface on the [health](/architecture/health/) side as `unknown` reasons:
a went-stale datapoint is the `stale` reason, and a covered-but-never-reported datapoint is the
`no-data` reason (distinct from `uncovered`, where no health-impacting rule resolves at all).

**Cadence is inferred for pollers, declared for heartbeats.** A poller's expected interval is its
`interval` times a tolerance. A listen-triggered function is **opt-in**: watched only if it declares
an expected heartbeat interval (an MQTT keepalive, a source that pings); silence on a listener
with no declared heartbeat is normal and unwatched.

:::caution[Open question]
The watchdog tolerance defaults (the multiplier on a poller's `interval`) and whether to debounce a
missed-poll burst before declaring stale.
:::

## Timezones

Every stored instant is a **`timestamptz`** (UTC, tz-aware), universal everywhere. A **`schedule`
additionally carries an IANA timezone** (`America/New_York`) for computing recurrence and calendar
boundaries, because DST means "8am" and "the 1st of the month" cannot be precomputed as fixed
offsets. The resolved `fire_at` is a `timestamptz`; the recurrence is computed in the schedule's
timezone.

## Digests

A digest is a **schedule that fires an aggregating action**: the `origin=scheduled` event triggers
an `action_rule` whose action queries (open alarms, the day's events), renders a Go-template body
([alarms and actions](/architecture/alarms-actions/)), and sends. No new machinery: schedule plus
action, composed.

## Storage

The recurring trigger config and the clock singleton's pending-fire working set; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `schedule` | id, rrule/cron, **tz (IANA)**, target, enabled | config: a recurring trigger |
| `timer` | id, **fire_at (timestamptz)**, kind (schedule-tick / for-sustain / runbook-wait / watchdog), ref, payload | the clock singleton's pending-fire **working-set** (the durable PG working set, mutable, scanned for due rows and the fire realized on its lane: a record-lane fire born in PG and CDC-fanned to JetStream, a watchdog's staleness onto the data lane), not a history log; fires are logged on the entity they produce |
