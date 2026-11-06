---
title: "The metric lane is partitioned by UTC day"
date: 2026-11-06
status: Accepted
summary: "metric is range partitioned by UTC day (metric_pYYYYMMDD), not by month, amending ADR-1275. The server keeps today and the next three days partitioned and refuses to start ingest until every instant through then is covered; a routing miss is still retried. Retention drops a whole day once it is past the lane's window and the trend's compacted mark and holds no sample a longer type window still reaches. One create-copy-swap moved the months' rows, refusing unless the counts agree."
refs: [1527, 1275, 1276, 420]
---

- **Date:** 2026-11-06 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1527, amending [ADR-1275](#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month) points 2, 5 and 6.)
  1. **A day, not a month.** `metric` is range partitioned by UTC calendar day on `ts`, each
     partition named `metric_pYYYYMMDD`. Its three indexes, the series dimension and
     `metric_declared` (a plain table) are unchanged.
  2. **One create-copy-swap.** One migration renames the month-partitioned table (with its identity
     sequence) aside, creates the day-partitioned parent `LIKE` it (so the columns, their types,
     defaults and identity are metric's as they stand when the swap runs, not restated from
     #1275), a partition for every day it held a sample in and today through three days ahead,
     copies every row keeping its id, moves the identity past the old table's, refuses unless the
     counts agree, drops the month table, and makes its indexes again on the new parent from their
     own definitions and names. Its down is the same swap back to months.
  3. **The roll.** The server keeps today and the next three days partitioned, at boot and on every
     retention pass. After creating any missing day it reads the partitions back and refuses unless
     they cover every instant from the start of today through the end of the third day ahead, naming
     the first uncovered instant; the boot starts no consumer over a refusal. A partition-routing
     miss (SQLSTATE 23514 naming no constraint) is still made and retried, never refused.
  4. **Retention by the day.** A day is dropped whole (detached `CONCURRENTLY`, then dropped) once
     it lies wholly before the lane's window and the trend's compacted mark
     ([ADR-1276](#adr-1276-hourly-trends-close-an-hour-a-day-after-it-ends-and-hold-back-the-prune)),
     unless it holds a sample of a type whose own `retention_days` window still reaches into it. A
     held day stays, and the chunked prune keeps each window exact inside it. Where ADR-1275 held
     every month back to the longest type window installed, a day is held only by a series that
     has a sample in it.
- **Context:** a month partition outlives the window by up to a month, so most of what the prune
  removes it removes chunk by chunk, writing WAL and dead tuples for rows a drop would remove for
  free. A day bounds that to one day past the window. `make footprint`, on the same host and build
  before and after: heap unchanged at 136 rows a page (60.2 bytes a row), index after `REINDEX`
  40.5 to 38.1 bytes a row, live index 61.9 both, WAL 309.5 to 310.2 bytes an observation,
  statements per batch unchanged at 18.
- **Not chosen:**
  - **A per-type partition key.** It would let a longer type's days drop on their own schedule, but
    it multiplies the partitions by the overridden types and moves a series between partitions
    when its type's window changes.
  - **Holding every day back to the longest type window,** as the months did: one type kept for a
    year would hold a year of every type's days, and the chunked prune would do the work the drop
    is for.
