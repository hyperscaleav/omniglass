---
title: "A driver poll runs on its declared schedule, at heartbeat granularity"
date: 2026-10-23
status: "Accepted"
summary: "A driver poll runs on its declared `schedule.every` at heartbeat granularity (#822): a per-task next-due time kept for the run, the first tick at or after it runs the poll, a new task runs at once, a late tick never bursts; probes stay per tick and pushes are never gated. Settles the nodes page's open question for driver polls"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [collection](/architecture/collection/), [drivers](/guides/admin/drivers/)
- **Decision:** The node honors a driver poll's `schedule.every` (#822). It keeps, per task id and for
  the life of the run, when each poll is next due, and runs the poll on the first heartbeat tick at or
  after that time. The next run is booked from the due time, so tick jitter does not drift the cadence;
  a tick so late it missed whole intervals runs the poll once and books from now, never a catch-up
  burst. A run that lands nothing because its fetch failed retries on a backoff instead of waiting
  its interval: `min(every, max(heartbeat, backoff))`, the backoff starting at the heartbeat and
  doubling per consecutive failure, so a 24h poll that fails during a device reboot retries within
  seconds and a dead device is still asked no more often than every heartbeat; the first success
  books from its due time again. A down held session is proven by a listener's push as well as by
  an answered poll, so a gated poll never keeps a pushing device reading down. A task new to the
  worklist runs at once; a task that leaves it is forgotten; a restart polls
  at its first tick. The gate covers driver polls only: reachability probes still run every tick, and a
  listener's pushes are never gated, since they ride the held session as they arrive.
- **Context:** The schedule was validated and baked but unread, so `snmp-generic`'s `every: 60s` polled
  every 30s heartbeat and a `24h` poll would have asked a device 2880 times a day. The nodes page left
  per-task cadence within a tick as an open question; a per-task gate settles it for driver polls
  without the tick scheduler's redesign (the overrun-aware loop and the bounded pool stay design), at
  the cost of cadence precision bounded by the heartbeat.
