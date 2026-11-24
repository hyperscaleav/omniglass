---
title: Time
description: "How time is handled: the server's periodic loops that act on time passing today, the timestamp rules every lane follows, and the clock primitive that will turn the passage of time into events."
sidebar:
  order: 45
  badge:
    text: Partial
    variant: note
---

Everything else in the pipeline is push-driven: a sample arrives, and something reacts. Time is the one
input that **arrives as nothing**. "Ten minutes have passed", "it is 8am on Monday" and above all "the
data has stopped" produce no inbound message, so nothing would ever react to them unless the platform
manufactures the reaction itself. This page covers how the server does that today, the rules every
stored instant follows, and the one primitive that is to replace today's separate loops.

## Today: a loop per job

Time-driven work runs as independent loops on each server, each a goroutine with its own interval and
its own deadline per pass. Every one keeps its state in Postgres rows, so a restart loses nothing, and
every queued job is taken `FOR UPDATE SKIP LOCKED`, so replicas share the work without doing any of it
twice.

| loop | every | what time passing does |
|---|---|---|
| event rule sweep, with command settlement | 5 s | fires or clears a rule whose [sustain](/architecture/alarms-actions/#judged-as-samples-land) has run out on an input that reports only on change; settles or times out [commands](/architecture/commands/) |
| rule reconcile | 1 min | re-judges each rule's newest sample, and every trend rule's window, healing a missed decision |
| calc tick | 1 s | drains pending [calc rule](/architecture/calculations/#how-a-calc-rule-runs) marks, and marks owners whose windowed input has aged out |
| notification tick | 2 s | sends due [notifications](/architecture/alarms-actions/#delivery-the-notification-row) and their retries, and ends expired [snoozes](/architecture/alarms-actions/#snooze-silencing-an-alarm-for-a-while), announcing an alarm still raised |
| outpost check-in age | 15 s | writes each claimed outpost's `checkin-age`, which `outpost-silent` reads |
| sweep ages | 10 s | writes each server's `rule-sweep-age`, which `rule-sweep-stalled` reads |
| sample retention | 1 h | drops and summarizes what has passed its window ([storage](/architecture/storage/#what-a-retention-pass-reports)) |

The pattern that makes absence visible is the **age**: rather than a timer that fires when something
stops, a loop writes how long it has been since something happened, as an ordinary metric, and an
ordinary event rule fires when the age passes a line. `outpost-silent` (a check-in older than 90 s) and
`rule-sweep-stalled` (a sweep older than 30 s) are built that way, so silence raises an alarm through
the same machinery as any other signal.

**A sustain is timed by the server's receipt of each sample**, never by the sample's own timestamp, so
an outpost whose clock runs behind neither skips nor stretches a sustain. Which sample is newest is still
decided by its timestamp.

## Instants and timezones

Every stored instant is a `timestamptz`: UTC, aware of its zone. A sample's `ts` is the producer's
clock; the server stamps its own receipt where that matters (a sustain, a batch receipt).

### A metric's timestamp is whole milliseconds

A metric sample's `ts` carries no digits below the millisecond, on `metric` and `metric_declared`, from
the outpost to the stored row (#1526):

- **The outpost** truncates every metric sample's `ts` to the millisecond as a batch leaves its spool,
  so no probe or driver has to remember to.
- **The ingest** truncates again before the row lands, rather than refusing, so a spool replayed from an
  older outpost lands instead of failing its batch.

The reason is the clock, not the storage. An outpost's clock is NTP-synced to a few milliseconds, so
microsecond digits are noise, and a raw row and the compacted block that later holds it (millisecond
offsets, #1265) then agree exactly. Two samples of one series may land in the same instant: there is no
unique key on `(series_id, ts)` ([ADR-0061](/architecture/decisions/#adr-0061-a-calculated-series-is-current-at-its-highest-id-not-its-newest-timestamp)), and ids tell them apart. The
property, event and log lanes keep their timestamps as sent.

:::design[The clock primitive, tracked in #419]
One firing engine replaces the loops above, so new background work stops accreting as independent
tickers. It holds two things:

- a **`schedule`** (config): a recurring definition, a cron or recurrence rule plus an IANA timezone,
  and what it triggers;
- a **`timer`** (a working set, not a history): every pending fire, with a `fire_at`, a kind and a
  pointer to what it is for. A mutable row set like a queue, scanned for due rows.

Three patterns ride that one mechanism:

- **recurring** (a schedule): fire, then reschedule. Digests, synthetic checks, calendar resets of a
  service level window.
- **armed and cancellable** (a one-shot): armed by an event, fired later, cancelled if the cause goes
  away. A sustain, a flow's `wait` step, an escalation delay.
- **reset on arrival** (a watchdog): pushed to now plus a tolerance on each sample, fired if it lapses.
  No-data and staleness.

One replica fires each job, under a lease, and a fire missed while the server was down follows its
register's policy: a sweep runs once to catch up, a schedule fires late only while still meaningful, an
armed timer fires late unconditionally.

**A fire is recorded once, on the log of what it produces.** A schedule's fire is an ordinary event with
`origin = scheduled`, written in a Postgres transaction like any other, so an action rule reacts to it
the way it reacts to any event:

```yaml
action_rule:
  on: event
  when: 'origin == "scheduled" && schedule == "daily-digest"'
  action: email-open-alarms-summary
```

A sustain's fire is the alarm's edge, a flow's wait advances its action, and a watchdog's lapse is a
derived staleness sample. Housekeeping fires never write an event per interval, or the event log becomes
a cron log.

**Schedules are ground truth; gaps are derived.** A backtest does not re-run the clock: it reads the
recorded scheduled events as they are. A data gap is already in the recorded data (absent rows in a
window), so a backtest re-detects it with no clock.

**A schedule carries its IANA timezone** (`America/New_York`), because DST means "8am" and "the 1st of
the month" cannot be precomputed as fixed offsets; its resolved `fire_at` is a `timestamptz`.
:::

:::caution[Open question]
The recurrence a `schedule` accepts: a full iCalendar recurrence rule, or a cron subset plus calendar
anchors such as month start and month end.
:::

:::design[No data: stale and unknown, tracked in #2608]
Absence of data is two conditions, and [health](/architecture/health/) treats them differently:

- **stale**: there was a value and it has aged past its expected cadence. The watchdog's product, so it
  can arm only after a first arrival; the last value and its age are kept, and the current value carries
  a `freshness` quality (`fresh` or `stale`). Usually actionable: a stopped signal most often means lost
  visibility.
- **unknown**: never observed, no baseline and no last value. A "not monitored yet" state, detected by
  the absence of any observation rather than by a watchdog, and not actionable.

**Cadence is inferred for pollers and declared for listeners.** A poller's expected interval is its
`interval` times a tolerance. A listen-triggered input is watched only if it declares an expected
heartbeat (an MQTT keepalive); undeclared silence is normal. Whether a stale value is still valid or
means lost visibility is the property type's policy, through its declared staleness tolerance.
:::

## Storage

Time has no table of its own today: each loop's state is the row it works on (a streak, a calc mark, a
notification, a snooze), on that subsystem's page. The clock adds two tables, laid out on
[storage](/architecture/storage/).

:::design[The clock's tables, tracked in #419]
| Table | Key columns | Notes |
|---|---|---|
| `schedule` | id, recurrence, tz (IANA), target, enabled | config: a recurring trigger |
| `timer` | id, fire_at, kind, ref, payload | the pending-fire working set, scanned for due rows; a fire is logged on what it produces, never here |
:::
