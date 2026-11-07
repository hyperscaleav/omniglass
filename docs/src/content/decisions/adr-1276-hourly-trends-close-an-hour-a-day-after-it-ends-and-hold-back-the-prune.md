---
title: "Hourly trends close an hour a day after it ends, and hold back the prune"
date: 2026-11-05
status: Accepted
summary: "The trend tier is metric_trend, one row per observed metric series per UTC hour (count, min, max, sum, first, last), written by the retention pass's compaction once an hour ended a day ago, and kept for retention.trend_days. Two marks record how far hours are announced and how far they are folded: the compaction moves the announced mark, takes an advisory lock every metric insert holds shared to wait out the inserts in flight, and folds each hour in a transaction that moves the folded mark past it. A late sample marks its hour dirty in its own insert and a later compaction recomputes it. The metric prune and the month drop never pass the folded mark."
refs: [1276, 1265, 1275, 311, 1269]
---

- **Date:** 2026-11-05 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [admin guide](/guides/admin/)
- **Decision:** (#1276, under the approved #1265.)
  1. **One trend row per observed series per UTC hour**, `metric_trend (series_id, hour, count, min,
     max, sum, first, last)`, keyed by series and hour and removed with its series. Calculated series
     are not compacted; properties have no trends.
  2. **An hour closes a day after it ends**, the late-data horizon ([ADR-0311](#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)),
     not when its month's partition ends, as #1265 first wrote: a month partition holds samples past
     the 14-day hot window, which the chunked prune removes inside the month long before it ends, so
     the trend has to exist first.
  3. **Two marks, and a lock for the writes in flight.** `metric_trend_mark` holds an announced mark
     (`announced_through`, how far the compaction has said it will close) and a compacted mark
     (`compacted_through`, how far it has folded). A pass moves the announced mark and commits, then
     takes the `metric_trend:announce` advisory lock exclusive and lets it go at once. Every metric
     insert reads the announced mark through `metric_trend_announced()`, which takes that lock shared
     until the insert's transaction ends and then reads the mark on a fresh snapshot, so the exclusive
     lock waits for every insert that may have read the old mark, and every insert after it reads
     the new one. Then each hour folds in a transaction of its own that moves the compacted mark past
     it. A pass that stops anywhere (a cancel in the drain or the fold, a failed fold, a crash) leaves
     every hour before the compacted mark folded and every hour after it open for the next pass; the
     announced mark stays ahead, which only marks a few hours dirty for a needless recompute. An insert that holds
     the lock past 5 seconds fails the pass (a new insert queues behind the waiting compaction, so the
     bound is also the longest the ingest stalls). Only metric inserts take the lock: an open
     transaction of any other kind, or in another database, stalls nothing. Up to 48 hours close a
     pass; a pass left behind brings the next one in a minute. The first pass starts at the oldest month
     the hot tier holds or the longest window back from now (the metric lane's or a type's own),
     whichever is later, so an upgrade does not fold years of hours the prune is about to remove.
  4. **A late sample dirties its hour in its own insert** (the `calc_pending` shape #1276 named): the
     metric lane's one statement gains a final clause marking `(series, hour)` in `metric_trend_dirty`
     when an observed sample's `ts` is before the announced mark, so statements per batch stay 18. A
     compaction recomputes each dirtied hour once the compacted mark is past it, each dirtied hour whole from its samples, which makes a recompute
     idempotent; an hour whose samples are already past its type's window is not recomputed (only part
     of it would remain). The recompute holds the trend lock exclusive from before it reads the
     samples, up to 500 dirtied hours a transaction: a second late batch into an hour already marked
     hits the mark `on conflict do nothing` and locks no row, so only the lock orders it against the
     recompute that clears the mark (a versioned mark the insert bumps was the alternative, and costs
     a row lock and a write per late batch). The sample write, the fold and the recompute run at READ
     COMMITTED whatever the server's `default_transaction_isolation`, since each must read, after
     the lock is granted, what committed before it.
  5. **The prune waits for the trend.** The metric lane's chunked prune and month drop cap their cutoff
     at the compacted mark, and prune nothing before the first compaction, so no hour leaves the hot tier
     without its trend. The trends are pruned to `retention.trend_days` like any lane, a window of their
     own that nothing forces to exceed `retention.metric_days`.
  6. **Reads come with #1269**, whose bucketed series read reads trends beyond the hot window.
- **Context:** #1265's acceptance asks for a 30-day chart that still shows a one-sample spike after the
  hot tier has kept only 14 days; a bucket's maximum carries the spike. Measured on PG18 after `VACUUM`,
  a trend row costs about 145 bytes with its primary key, 3,482 bytes per series per day
  (`TestTrendFootprint`), against about 100 bytes per sample in the hot tier (ADR-1275).
