---
title: "Each poll and probe runs on its own interval, from a dispatcher of its own"
date: 2026-11-09
status: "Accepted"
summary: "A node's polls and probes leave the heartbeat tick for a dispatcher with its own timer (#1634): each task is due on a stable phase of its own interval (fnv of its id over a Unix-aligned grid), runs at once when new or when its fingerprint moves, and is handed to its endpoint's lane on a bounded worker pool (`--poll-workers`, default 16), operator work first. A due whose run is still in flight is skipped and counted, never queued; a late pass runs a task once. The retry backoff starts at one second, not the heartbeat; a poll with no schedule runs at the heartbeat interval; `--probe-every` defaults to 30s and no longer means every check-in. Amends ADR-0152 and ADR-1273"
refs: [1634, 1624, 1637, 822, 1273, 1249, 1625, 1649]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#scheduling-concurrency-and-self-observability), [collection](/architecture/collection/), [lab](/guides/lab/)
- **Decision:** (#1634, under the node concurrency model of ADR-1637, #1658.)
  1. **A dispatcher with a timer of its own.** Every poll and probe task sits once in a due-time
     heap. The dispatcher sleeps until the earliest due (or a worklist change wakes it), hands each
     due task to its endpoint's lane and books the next due. The heartbeat no longer runs tasks: it
     publishes liveness, pulls commands, ships the spool and takes the dispatcher's window.
  2. **A stable phase.** A task's dues fall on a grid one interval apart, offset from the Unix epoch
     by an fnv-1a hash of the task id. A new task, and every task on a node's start, runs at once;
     its next due is the first grid slot at least half an interval on, then one interval apart. A
     restart lands each task on the same slots, and a fleet of tasks with one interval spreads across
     it.
  3. **Skip, not burst.** A due whose last run is still in flight is skipped and counted, in the
     endpoint's report window (`poll-due-count`, `poll-skipped-count`, skipped over due is schedule
     loss) and the dispatcher's own (`tick-overrun-count`, `schedule-skip-count`). A late pass runs a
     task once and books the next slot ahead of now; the slots it passed over count as skips.
  4. **Lanes on a bounded pool.** A lane is an endpoint: one job at a time per endpoint, endpoints
     side by side, on `--poll-workers` workers (default 16, `OMNIGLASS_POLL_WORKERS`). An operator's
     run now or command takes its endpoint's lane ahead of the scheduled work queued there, never
     preempting the run in flight. A component-wide lane for a fragile device is #1649.
  5. **Intervals.** A driver poll's interval is its `schedule.every`; one with no schedule runs at the
     heartbeat interval, still on the dispatcher's timer. A probe's interval is `--probe-every`,
     default 30s; `0` is refused, so a probe never again runs on every check-in. A moved connection
     fingerprint runs the task at once (#1061), probes included.
  6. **Backoff from a fixed base.** A failed fetch retries after one second, doubling per consecutive
     failure, capped at the interval, never reading the heartbeat; a `Retry-After` is a floor (#902).
     A refused poll waits its interval unless its session reads up first, and a poll its endpoint's
     gate holds is decided in the pass without taking a lane
     ([ADR-1650](/architecture/decisions/#adr-1650-an-endpoint-gate-holds-a-set-of-reasons)).
  7. **The dispatcher keeps its own account.** Per heartbeat window: the longest pass
     (`tick-duration`), dues that met their run in flight (`tick-overrun-count`), dues passed over
     (`schedule-skip-count`), the latest dispatch past its due (`dispatch-late-ms`), and the busy and
     waiting high-water marks of the pool (`worker-busy-count`, `dispatch-queue-depth`). Today it says
     them in a `dispatch window` self-log line, only for a window that passed a due over; landing them
     as the node's platform metrics is #1625.
- **Amends:** [ADR-0152](/architecture/decisions/#adr-0152-a-driver-poll-runs-on-its-declared-schedule-at-heartbeat-granularity)
  (heartbeat granularity, the heartbeat-based backoff, a no-schedule poll on every tick) and
  [ADR-1273](/architecture/decisions/#adr-1273-a-reachability-probe-runs-on-the-nodes-probe-interval-not-every-heartbeat)
  (probe-every unset as every tick, the half-heartbeat due tolerance, the walkthrough lab probing on
  every check-in, which now probes every 2s by its own flag).
- **Context:** At heartbeat granularity a poll's interval rounded up to a heartbeat multiple (a 45s
  poll on a 30s heartbeat ran every 60s, a 10s poll every 30s), missed intervals on a late tick
  vanished uncounted, and every task ran serially inside the tick, so one dead device's timeout
  delayed every other component's poll. Making each task's own interval the clock removes the
  rounding, makes falling behind a counted loss, and lets a slow endpoint hold only its own lane.
  The concurrency model it runs under (one goroutine per concern, bounded pools, lanes) is ADR-1637.
- **Not chosen:**
  - **A ticker per task.** Thousands of timers for a fleet, and no one place to count lateness or
    skips; one heap and one timer is the same precision at a fraction of the cost.
  - **Queueing an overrun's due behind the run in flight.** It hides the overload and bursts a slow
    device when it recovers; a counted skip makes falling behind visible as loss.
  - **Phase from the first run.** A restart would move every task's slots, and tasks synced together
    would fire together; a hash of the id keeps both stable.
  - **Keeping `--probe-every 0` as every check-in.** It tied the probe to the heartbeat this decision
    removes; a lab that wants fast probes says so with its own interval.
