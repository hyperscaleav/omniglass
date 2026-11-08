---
title: "One series read serves raw rows and folded blocks (#1572)"
date: 2026-11-08
refs: [1572, 1530, 1529, 1566, 1573, 1574]
---

The first slice of #1530: every reader of a metric series' samples now reads them through one Storage
Gateway read over raw `metric` rows and unnested `metric_block` rows. The bucketed read at every
resolution, the raw read, the series API and the CLI answer the same before a day folds and after, and
the same again with the folded days' raw rows gone, which only a read of the blocks can. A late sample
lands raw and merges into its block, so the read takes each day of each lineage from one source: its
blocks when a block of its lineage covers it and the day is intact, its raw rows otherwise. Each
sample comes back with its lineage, by `ts`, then lineage, then landing order.

ADR-1530 records the choice, and amends #1530's spec, which said the bucketed reads were unchanged:
`ListMetricBuckets` read raw rows only. A bucket's last now breaks a tie at its newest instant by series
and lineage rather than by raw row id. No raw row is dropped yet; the trend recompute's move onto the
read is #1573, the pending table and the early drop are #1574, and trend-served buckets are #1566.
