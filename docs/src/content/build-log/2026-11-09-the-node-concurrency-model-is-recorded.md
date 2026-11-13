---
title: "The node's concurrency model is recorded: per-kind queues on their own timers (#1637)"
date: 2026-11-09
refs: [1637, 1624]
---

ADR-1637 records the model the children of #1624 build to, before any of it is built. The node's one
heartbeat loop gives way to per-kind queues, each on its own timer with a bounded pool: the heartbeat
(liveness only), the worklist pull (already on its own interval, ADR-1577), polls and probes (#1634),
commands woken by a server push with a fallback pull (#1635), held sessions as supervised goroutines
(#1636), and housekeeping. The endpoint is the one exclusion key: discrete requests to an endpoint
serialize in its lane, operator work ahead of scheduled work, and listen tasks are never serialized.
One endpoint gate holds a set of reasons (`refused` today; `unreachable`, `disrupted` and `upstream`
next, #1650, #1651, #1652) that every lane consults and none holds. A task runs on its own interval
and a run still in flight at its next due is skipped and counted, never burst; lateness is published
(#1625). A telemetry batch is the spool's flush window, not a tick.

The nodes page's "Running tasks", "The component task queue", "Implicit reachability" and the
scheduling section (renamed from "Tick scheduling, concurrency, and self-observability") describe the
model inside design fences tracked to those children; the overrun-aware loop is replaced by
skip-not-burst, the post-command wait becomes the gate's `disrupted` reason, and `node.self` and the
node-down sweep stay with #430 and #1094. ADR-0152, ADR-1273, ADR-0136 and ADR-0311 carry one-line
amendments pointing at the new decision. Nothing is built by this change; the page stays `Partial`.
