---
title: "Each poll and probe runs on its own interval, not on the heartbeat (#1634)"
date: 2026-11-09
refs: [1634, 1624, 1625, 1649]
---

A node's polls and probes now run from a dispatcher with a timer of its own instead of inside the
heartbeat tick ([ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own)).
Each task is due on a stable phase of its own interval, hashed from its id so a restart keeps it and
a fleet spreads out; a new task or a moved fingerprint runs at once. Due tasks go to their endpoint's
lane on a bounded worker pool (`--poll-workers`, default 16), one job per endpoint at a time,
operator work first, so a dead device holds only its own lane. A due whose run is still in flight is
skipped and counted (`poll-due-count` and `poll-skipped-count` per endpoint, skipped over due is
schedule loss), never queued to burst later. The retry backoff starts at one second rather than the
heartbeat, a poll with no schedule runs at the heartbeat interval, and `--probe-every` defaults to
30s, so no probe runs on every check-in. Proven over the real wire
(`TestAPollRunsOnItsOwnIntervalBesideADeadDevice`): on a 30s heartbeat a 10s poll lands its samples
10s apart within a second beside a device that never answers, and 60 polls across ten components
run concurrently under the race detector. The dispatcher's window (pass duration, overruns, skips,
lateness, busy workers, queue depth) is a `dispatch window` self-log line for now; landing it as the
node's platform metrics is #1625. Building it surfaced a data race in the https prober, whose dial
could write its result after a timed-out request returned; the dial now writes under a lock.
