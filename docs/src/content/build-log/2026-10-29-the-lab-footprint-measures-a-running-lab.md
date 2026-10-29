---
title: "The lab footprint measures a running lab, per container (#1277)"
date: 2026-10-29
refs: [1277, 1272, 1265]
---

The ingest footprint (#1272) counts what one node batch costs the database, but not what a lab
costs to run. `make lab-footprint` now measures that. It brings up a throwaway lab at a cadence
profile and warms it up. Over N windows it then reads each container's CPU and working set from
its cgroup, rows a minute per lane, and the growth a day of each table, the database and the WAL.
Last it times the 24 h and 30 d series reads with the server and Postgres held to one vCPU. Each
figure is the median, with the lowest and highest window beside it. It is a diagnostic, never a
gate ([ADR-1277](/architecture/decisions/#adr-1277-the-lab-footprint-measures-a-running-lab-per-container-and-gates-nothing)).
When it finishes it stops the lab and checks that nothing of its project is left.

The lab's node cadence is now a named profile: `lab` (a learner's), `walkthrough` (a recording's,
probing on every check-in) and `default` (the product's 30 s). The lab guide renders the profiles
from the planner.

The first reading, on build `ccbb5d56`, the dev fleet (41 systems, 215 components, 609 tasks): a
2-minute warm-up, then 5 windows per profile (one minute each, two at `default`, so a window
holds four of its check-ins), run back to back. The host was shared, with a load average of 6 to
17 on 12 cores, so CPU reads high and the ranges are wide. At `default` the simulator re-pulls its
list at its own 10 s rather than the lab's 2 s, so the figure carries as little simulator traffic
as a lab can.

| Per lab | `walkthrough` (2 s, probe every check-in) | `lab` (2 s, probe every 10 s) | `default` (30 s) |
|---|---|---|---|
| Metric rows a minute | 13,257 (13,000 to 13,342) | 2,930 (2,929 to 2,961) | 1,205 (1,044 to 1,366) |
| Metric table a day, with indexes | 4.21 GiB | 966 MiB | 376 MiB |
| Database a day | 5.42 GiB | 1.25 GiB | 556 MiB |
| WAL a day | 11.77 GiB | 2.71 GiB | 1.50 GiB |
| CPU, server / node / db (millicores) | 222 / 88 / 289 | 114 / 28 / 145 | 28 / 13 / 90 |
| CPU in all | 596 (580 to 643) | 289 (285 to 313) | 131 (129 to 133) |
| Memory, server / node / db (MiB) | 44 / 128 / 100 | 42 / 130 / 79 | 41 / 134 / 76 |
| Series read, 24 h / 30 d, one vCPU | 3.9 / 4.2 ms | 3.5 / 2.9 ms | 2.9 / 2.8 ms |

This reproduces the 2026-10-27 hand measurement at 2 s, about 13,000 rows a minute. It also
reproduces the 2026-10-28 learner's lab figure of 2,733. At 30 s it reads 1,205 against about
1,000, because the fleet has grown since. CPU at 2 s reads 596 against about 470, on a host this
busy. The reads are fast because a lab minutes old holds minutes of history; the 1,000-system lab
(#1357) measures them on volume.
