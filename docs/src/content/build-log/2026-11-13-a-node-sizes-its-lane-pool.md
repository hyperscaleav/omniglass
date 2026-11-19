---
title: A node sizes its lane pool from its CPU and memory (#1869)
date: 2026-11-13
refs: [1869, 1892, 1249, 1623]
---

`--lane-workers` now defaults to `auto` instead of 16. A node reads the CPU and memory it may use
from its cgroup (`cpu.max` and `memory.max`, the tightest of its own and its parents'), holds each
to the host's (`GOMAXPROCS` where it is below the CPU count, `MemTotal`), and sizes its endpoint lane pool to the tighter of a CPU bound
and a memory bound, clamped to 4 to 256. The CPU bound weighs a run's wait on its device against its
CPU cost, since a lane mostly waits: at the measured 4.4 ms of CPU and 200 ms of wait, about 23 lanes
a CPU on half the CPUs. A number on the flag or in `OMNIGLASS_LANE_WORKERS` still sets the pool, and
anything that is neither a count of at least one nor `auto` is refused at start.

The node logs `lane workers` at boot, before it claims, with the count and its source (`set`, or `auto:` and the limits
it read with where each came from), and reports the count as a new node-self metric,
`lane-worker-count` under the instance `lane`, once per report window beside `worker-busy-fraction`.

Proven by unit tests of the sizing over 2-core, 4-core, 32-core, fractional and memory-bound limits
and both clamps, the cgroup reader over fixture trees (a parent's limit binding its child, `max` and
unreadable files as no limit), the `/proc/self/cgroup` and `/proc/meminfo` parsers, the host
bound and `GOMAXPROCS`, the operator's count winning over auto, the CLI's refusal of a bad count, and the node
report emitting the count once per window.
A lab integration test runs the node image under `docker run --cpus 0.5 --memory 64m` and
`--cpus 4 --memory 32m`, so the kernel writes the limits the node reads, and asserts the boot log's
11 and 8 lanes and their sources.

The lab now caps every node it starts (#1892): one CPU, so auto gives each the same 23 lanes on any
host and a scale run's parallelism is one figure, and memory at twice the cost model's figure for its
site's components, at least 512 MiB, with `GOMEMLIMIT` a tenth under it. The 72-hour corporate 3,000
soak set the figures: each node's resident memory sat on the model's line (about 0.8 to 1.0 MiB a
component over 25 MiB), and the busiest node used 0.43 of a CPU. The server, Postgres and the seed
stay uncapped, and the lab guide's topology table gains a Limits column from the plan. Proven by
unit tests that the plan caps every node and only the nodes, the memory figure at the floor and at
a regional and the hq, the `GOMEMLIMIT` figure, the caps reaching Docker with swap held to the memory
cap, and the compose config hash carrying the caps; and by the lab integration test, whose node logs
23 lanes from `cpu.max=1` and `memory.max=512MiB`.
