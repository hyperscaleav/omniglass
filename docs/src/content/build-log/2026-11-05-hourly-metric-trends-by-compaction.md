---
title: "Hourly metric trends by compaction (#1276)"
date: 2026-11-05
refs: [1276, 1265]
---

Every observed metric series now keeps one row per hour in `metric_trend` (count, minimum, maximum,
sum, first and last value), written by the retention pass a day after the hour ends and kept for
`retention.trend_days` (two years by default). A one-sample spike survives as its hour's maximum after
the samples themselves are pruned at 14 days, under the default windows (each is set on its own).

The compaction announces the hours it is about to close, waits on an advisory lock every metric insert
holds shared for the inserts already in flight, so no sample slips between, and then folds each hour in
a transaction that moves a second, folded mark past it, so a pass cancelled anywhere skips no hour. A
long transaction elsewhere, or in another database, holds no such lock and stalls nothing. The first
pass starts at the longest window back, so an upgrade does not fold years of hours. A sample that arrives after its hour closed (a
spool replayed late) marks the hour in the same insert that lands it, and the next pass recomputes the
hour, holding the same lock so a second late batch into the hour is never folded out; batches still
cost 18 statements, at READ COMMITTED whatever the server's default. The metric prune and the month drop never pass the
folded mark. A day of trends costs about 3.5 KB per series. Reading them is the bucketed series
read, #1269.
