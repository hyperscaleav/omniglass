---
title: "A reachability probe runs on its own interval (#1273)"
date: 2026-10-28
refs: [1273, 1265]
---

A node ran every reachability probe on every heartbeat, so the lab, which checks in every 2s so a
change in the console reaches its node within seconds, probed every endpoint every 2s and wrote
most of its rows doing it. `node run --probe-every` now sets how often probes run, through the same
per-task schedule a driver poll uses; unset, a probe still runs on every heartbeat.

A learner's lab checks in every 2s and probes every 10s. The throwaway lab a walkthrough is recorded
on keeps probing every 2s, so a walkthrough that waits on a probe's verdict stays inside its time
budget ([ADR-1273](/architecture/decisions/#adr-1273-a-reachability-probe-runs-on-the-nodes-probe-interval-not-every-heartbeat)).

Its review found that the per-task schedule, which driver polls already used, skipped ticks at random
when an interval equalled the heartbeat: a run is booked from when its tick reached it, which varies
by milliseconds. A task is now due within half a heartbeat of its due time.
