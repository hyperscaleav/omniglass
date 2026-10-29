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

The first reading, on build `5cf55044`, the dev fleet (41 systems, 215 components, 609 tasks): a
2-minute warm-up, then 5 one-minute windows per profile, run back to back. The host was shared,
with a load average of 12 to 22 on 12 cores, so CPU reads high and the ranges are wide.

| Per lab | `walkthrough` (2 s, probe every check-in) | `lab` (2 s, probe every 10 s) | `default` (30 s) |
|---|---|---|---|
| Metric rows a minute | 13,138 (13,021 to 13,300) | 2,929 (2,928 to 2,960) | 1,225 (1,140 to 1,340) |
| Metric table a day, with indexes | 3.99 GiB | 965 MiB | 415 MiB |
| Database a day | 5.20 GiB | 1.27 GiB | 606 MiB |
| WAL a day | 11.49 GiB | 2.71 GiB | 1.28 GiB |
| CPU, server / node / db (millicores) | 211 / 82 / 284 | 116 / 30 / 144 | 33 / 13 / 105 |
| CPU in all | 576 (522 to 585) | 292 (268 to 310) | 150 (137 to 159) |
| Memory, server / node / db (MiB) | 47 / 135 / 92 | 41 / 128 / 84 | 37 / 129 / 70 |
| Series read, 24 h / 30 d, one vCPU | 8.9 / 6.4 ms | 5.2 / 3.2 ms | 2.8 / 2.1 ms |

This reproduces the 2026-10-27 hand measurement: about 13,000 rows a minute at 2 s, and the
2026-10-28 learner's lab figure of 2,733. At 30 s it reads 1,225 against about 1,000; the fleet
has grown since, and a one-minute window holds only two 30 s check-ins. CPU at 2 s reads 576
against about 470, on a host this busy. The reads are fast because a lab minutes old holds minutes
of history; the 1,000-system lab (#1357) measures them on volume.
