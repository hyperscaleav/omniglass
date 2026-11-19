---
title: "A node reads one busy fraction for its one lane pool"
date: 2026-11-13
status: "Accepted"
summary: "`worker-busy-fraction` lands under the single instance `lane`, the busy share of the one pool every kind of discrete work runs on today, rather than one instance per task kind as #1686 specified; it is busy worker time over the worker time the pool had, so a report window that spans a resized pool still reads 0 to 1. Per-kind instances arrive with per-kind pools"
refs: [1686, 1637, 1638, 1688]
---

- **Date:** 2026-11-13 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/)
- **Decision:** The node publishes `worker-busy-fraction` once per report window under the one instance `lane`: the worker time its lane pool spent running polls, probes, commands and runs, over the worker time the pool had (each dispatch window's length times its workers, summed). When a kind of work gets a pool of its own, that pool reads the same figure under its kind as the instance, and `lane` keeps the pool that is left.
- **Context:** #1686 specified one figure per scheduler pool, its kind as the instance, on the reading of [ADR-1637](#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time) that polls, probes, commands and sessions each run on a pool of their own. The node as built runs every discrete request through one lane pool (the endpoint lanes share its workers), so per-kind figures would split one pool's capacity among instances that do not own it, and their sum would not read 0 to 1. One instance names the pool that exists. The assignment reconcile (#1688) reads the node's headroom, which is this pool's. Dividing by the worker time the pool had, rather than by the latest worker count, keeps the figure at most 1 across a resize.
