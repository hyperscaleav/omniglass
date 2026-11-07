---
title: "Observed metric samples fold into lossless blocks (#1528)"
date: 2026-11-07
refs: [1528, 1265, 1525, 1276, 1529, 1530, 1531]
---

The compacted tier's first slice: each closed UTC day of an observed metric series now folds into
`metric_block` rows of millisecond offsets and values, in landing order, at the span its own sample
count earns. 4,080 samples or more in the day make hour blocks, 170 or more one day block, and a
sparser day waits for its ISO week's block, which has no minimum. The retention pass folds after the
trend compaction, on its marks, two days a pass, and each batch of series advances the block mark in
its own transaction, so a cancelled pass resumes where it stopped. A late sample marks its day in
`metric_block_dirty` in the same insert that marks its trend hour, and the next pass rebuilds the day
and its week block from the raw rows.

The blocks are written beside the raw rows and nothing reads them yet: every reader answers exactly
as before, the late merge without raw rows is #1529, and block reads, the early raw drop and the block
prune are #1530. `VerifyMetricBlocks` checks every block against its raw rows, sample by sample and
bit for bit. ADR-1528 records #1265's accepted design with its 2026-11-05 rulings, including a 30-day
`metric_days` default and per-type trends, which #1531 builds.

`make footprint`, one ISO week of #1525's generated series at its series counts, folded by the pass,
against #1525's 30 days folded in a staging query. The ingest is unchanged at 18 statements a batch.

| Cadence | Span | Samples | #1525, bytes a sample | Through the fold |
| --- | --- | --: | --: | --: |
| 10 s | hour | 616,783 | 7.71 | 7.82 |
| 30 s | day | 616,881 | 7.18 | 7.18 |
| 60 s | day | 616,788 | 7.25 | 7.37 |
| 5 min | day | 616,972 | 7.84 | 8.01 |
| 1 h | week | 514,147 | 8.58 | 8.25 |
