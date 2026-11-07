---
title: "Calculated metric series fold into blocks with their rule lineage (#1534)"
date: 2026-11-07
refs: [1534, 1528, 1529, 1276, 1530, 1531]
---

Calculated metric series now fold into `metric_block` as observed series do, and each block carries
the calc rule lineage its samples were written under: `source_rule`, `source_rule_id` and
`source_rule_version`, null on an observed block. A day splits at every rule edit or rename, and each
lineage earns its own hour, day or week span by its own count. The block key is a unique constraint
over series, span start, span and the lineage, with `NULLS NOT DISTINCT` so an observed span still has
one block. A late calculated sample merges into the block of its own lineage, or starts its week's
block when its version has none. `MetricBlockSamples` reads a range back from the blocks with each
sample's lineage, and `VerifyMetricBlocks` holds each block to its own lineage's raw rows. Calculated
series still have no trends; ADR-1534 records that lineage is the reason.

`make footprint` now reports a dense calculated series: 30 series at 30 s for one ISO week, the rule
edited mid-week, 616,881 samples in 240 blocks.

| | Bytes a sample |
| --- | --: |
| Calculated blocks, with lineage | 7.25 |
| Calculated raw rows, with lineage | 167.10 |

An observed block's three null lineage columns cost its row an 8-byte null bitmap, which moves the
observed figures by under 2% except at 5 min, where day blocks sit right at the size that fits four
to a heap page:

| Cadence | Span | #1528, through the fold | This slice |
| --- | --- | --: | --: |
| 10 s | hour | 7.82 | 7.84 |
| 30 s | day | 7.18 | 7.22 |
| 60 s | day | 7.37 | 7.38 |
| 5 min | day | 8.01 | 8.70 |
| 1 h | week | 8.25 | 8.36 |
