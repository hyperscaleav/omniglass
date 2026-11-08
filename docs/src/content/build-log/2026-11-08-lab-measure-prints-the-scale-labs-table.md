---
title: "omniglass lab measure prints the scale lab's table (#1556)"
date: 2026-11-08
refs: [1556, 1357, 1277, 1568, 1265, 1570, 1571]
---

The last slice of the scale lab: `omniglass lab measure` (and `make scale-measure`) runs against a scale
lab for a stated window and prints one table, then writes the same figures as JSON so two runs compare.
With `--systems N` it brings a throwaway lab up, measures it and removes it; without, it measures the
scale lab already running. How to run it and how to read each row is in the
[lab guide](/guides/lab/#measuring-a-scale-lab).

It extends the lab footprint (#1277) rather than starting over. Reused unchanged: the window arithmetic
(`WindowOf`, `UsageFromStats`), the database counters query (`DBCountersQuery`), the harness's
`psql`, `usage` and `get`, the ready-file wait, the teardown and leftovers check, and the table's
`count` and `size` formatting. New: the rate and extrapolation summary for a scale lab, nearest-rank
percentiles, the lag, redelivery and drain readers (`internal/labfootprint/scale*.go`), and the
`lab measure` command. `BenchmarkIngestFootprint`'s fleet parameter was not touched: the harness reads
a real lab's database, so a benchmark of the ingest path alone has nothing to add to a table of the
whole stack.

Two changes to the lab itself. The scale lab's Postgres starts with `track_commit_timestamp=on`, because
the ingest lag is a row's commit time minus its probe time and Postgres keeps the commit time only when
asked at start. And the lab's hold loop no longer calls a container stopped the instant it is seen down:
it waits up to 30 seconds for it to come back (`Runner.RestartGrace`), since the retention probe restarts
the server to run the pass and the lab tore itself down when it saw that. A crash stays down and is still
reported.

**Worker runs.**

12-core, 32 GiB shared worker, `corporate` scenario, one window of 3 minutes after a 2 minute warm-up
(table and WAL growth extrapolated from the window to a day). The baseline is the 41-system dev fleet
(#1265, 2026-10-28), which reported only the three figures it has.

| | 41-system baseline | 100 systems (3 sites, 322 components, 1,796 tasks) |
| --- | --- | --- |
| metric rows a minute | 2,733 | 7,997 |
| property rows a minute | | 6 |
| metric table a day | 0.85 GB | 1.15 GiB (1.24 GB) |
| receipts a day | | 1.20 GiB |
| database a day | | 2.36 GiB |
| WAL a day | | 5.43 GiB |
| server cpu | | 227 m |
| db cpu | | 233 m |
| node cpu (hq, regional, branch) | | 42 m, 14 m, 6 m |
| total cpu | 21% of a core (about 210 m) | 523 m |
| server, db memory | | 52 MiB, 183 MiB |
| node memory (hq, regional, branch) | | 193 MiB, 72 MiB, 39 MiB |
| total memory | | 539 MiB |
| ingest lag p50, p95 | | 462 ms, 1,737 ms (20,000 rows sampled) |
| consumer redeliveries (logged, a floor) | | 0 |
| fleet view p50, p95 | | 7.7 ms, 18.6 ms (20 reads) |
| retention drain (property lane) | | 150,000 rows in 1.7 s, 5.3 million a minute |

Per system the fleet writes about 80 metric rows a minute against the baseline's 67, so the 100-system
row rate is 2.9 times the baseline's for 2.4 times the systems. The metric table is 1.24 GB a day against
the baseline's 0.85 GB, 46% more for 2.9 times the rows (the two fleets differ in more than size, so this
is a comparison of order, not a regression). Two things stand out and are filed rather than tuned here (#1570):
the receipts table grows about as fast as the metric table (1.20 GiB a day against 1.15), and the
database's CPU is as high as the server's. The drain figure is quantized by the half-second poll and a
150,000-row backlog under one pass's budget, so it is a floor on the rate, not a limit.

**1,000 systems: pending #1577.**

The 1,000-system run (18 sites, 3,016 components) is not in this table. On the worker the lab's
bring-up did not reach ready: 17 of 18 sites and 2,210 of 3,016 components came up (the first site
at 91 s, the 17th at 168 s, with the seed taking 41 s), and the headquarters node (806 components)
never got its worklist, so it could not be measured (#1571). #1571 cut that node's pull from about
4,330 statements to 1,106, which was not enough; the pull is still a request the server answers one
at a time, and fixing that is #1577. The 1,000-system column is recorded when a bring-up reaches
ready, with `make scale-measure SYSTEMS=1000`.
