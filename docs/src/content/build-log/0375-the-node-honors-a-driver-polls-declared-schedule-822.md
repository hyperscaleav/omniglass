---
title: "The node honors a driver poll's declared schedule (#822)"
date: 2026-10-23
refs: [822]
---

Deferred from the stateless arc's review, closed in the stateful rollup. A driver poll's `schedule.every`
was validated and baked but unread, so every driver poll ran on every heartbeat. The node now keeps a
per-task next-due time for the run (`internal/node/schedule.go`) and runs a driver poll on the first
tick at or after it: a task new to the worklist runs at once, the cadence books from the due time so tick
jitter does not drift it, a late tick runs once rather than bursting, and a task that leaves the worklist
is forgotten. Reachability probes still run every tick, and a listener's pushes are never gated
([ADR-0152](/architecture/decisions/#adr-0152-a-driver-poll-runs-on-its-declared-schedule-at-heartbeat-granularity),
settling the nodes page's open question for driver polls). Proven by `TestPollScheduleRunsOncePerInterval`
(a 60s poll on a simulated 1s tick runs at 0, 60 and 120s) and `TestNodeHonorsThePollSchedule` (the real
node on a 50ms heartbeat asks a device about 4 times in 1.3s for a 400ms poll; the same test against an
ungated stub measured 26). The review that followed added the retry: a run whose fetch failed retries on
a backoff from the heartbeat, doubling, capped at the interval (`TestPollScheduleRetriesAFailureOnABackoff`;
it also let the simulator end-to-end test drop the start-order workaround the gate had forced on it),
pinned the anti-drift booking (`TestPollScheduleDoesNotDrift`), and let a listener's push prove a down
held session, which a gated poll could otherwise keep reading down for its whole interval
(`TestSessionDownReadsUpOnAPush`).
