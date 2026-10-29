---
title: "Hourly trends close an hour a day after it ends, and hold back the prune"
date: 2026-10-29
status: Accepted
summary: "The trend tier is metric_trend, one row per observed metric series per UTC hour (count, min, max, sum, first, last), written by the retention pass's compaction once an hour ended a day ago, and kept for retention.trend_days. A mark records how far hours are closed; the compaction moves it, then waits for the sample writes in flight. A late sample marks its hour dirty in its own insert and the next compaction recomputes it. The metric prune and the month drop never pass the mark."
refs: [1276, 1265, 1275, 311, 1269]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [admin guide](/guides/admin/)
- **Decision:** (#1276, under the approved #1265.)
  1. **One trend row per observed series per UTC hour**, `metric_trend (series_id, hour, count, min,
     max, sum, first, last)`, keyed by series and hour and removed with its series. Calculated series
     are not compacted; properties have no trends.
  2. **An hour closes a day after it ends**, the late-data horizon ([ADR-0311](#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)),
     not when its month's partition ends, as #1265 first wrote: a month partition holds samples past
     the 14-day hot window, which the chunked prune removes inside the month long before it ends, so
     the trend has to exist first.
  3. **A mark, announced before the fold.** `metric_trend_mark` holds how far hours are closed. The
     compaction moves the mark and commits, then waits until every transaction in flight at that
     commit has finished (each read the old mark), then folds the hours. A write that outlasts 30
     seconds puts the mark back and fails the pass, which the next pass retries. Up to 48 hours close
     a pass; a pass left behind brings the next one in a minute.
  4. **A late sample dirties its hour in its own insert** (the `calc_pending` shape #1276 named): the
     metric lane's one statement gains a final clause marking `(series, hour)` in `metric_trend_dirty`
     when an observed sample's `ts` is before the mark, so statements per batch stay 18. The next
     compaction recomputes each dirtied hour whole from its samples, which makes a recompute
     idempotent; an hour whose samples are already past its type's window is not recomputed (only part
     of it would remain).
  5. **The prune waits for the trend.** The metric lane's chunked prune and month drop cap their cutoff
     at the mark, and prune nothing before the first compaction, so no hour leaves the hot tier
     without its trend. The trends are pruned to `retention.trend_days` like any lane.
  6. **Reads come with #1269**, whose bucketed series read reads trends beyond the hot window.
- **Context:** #1265's acceptance asks for a 30-day chart that still shows a one-sample spike after the
  hot tier has kept only 14 days; a bucket's maximum carries the spike. Measured on PG18 after `VACUUM`,
  a trend row costs about 145 bytes with its primary key, 3,482 bytes per series per day
  (`TestTrendFootprint`), against about 100 bytes per sample in the hot tier (ADR-1275).
