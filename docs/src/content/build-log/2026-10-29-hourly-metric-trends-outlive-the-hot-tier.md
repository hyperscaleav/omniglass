---
title: "Hourly metric trends outlive the hot tier (#1276)"
date: 2026-10-29
refs: [1276, 1265]
---

Every observed metric series now keeps one row per hour in `metric_trend` (count, minimum, maximum,
sum, first and last value), written by the retention pass a day after the hour ends and kept for
`retention.trend_days` (two years by default). A one-sample spike survives as its hour's maximum after
the samples themselves are pruned at 14 days.

The compaction announces the hours it is about to close before folding them and waits for the sample
writes already in flight, so no sample slips between. A sample that arrives after its hour closed (a
spool replayed late) marks the hour in the same insert that lands it, and the next pass recomputes the
hour; batches still cost 18 statements. The metric prune and the month drop never pass the
compaction's mark. A day of trends costs about 3.5 KB per series. Reading them is the bucketed series
read, #1269.
