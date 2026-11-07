---
title: "Late metric samples merge into the block that covers them (#1529)"
date: 2026-11-07
refs: [1529, 1528, 1265, 1276, 1530]
---

A late observed sample for a day the block fold has passed now merges into the block that covers it,
in its own batch's transaction: the hour block of its hour, the day block of its day, or its ISO
week's block once the week has folded. Each block is rewritten once a batch from the block itself,
never from the raw rows, so the merge holds when #1530 drops raw days early. The folds and the merge
are ordered by a block advisory lock that only late batches take, so the on-time ingest is unchanged
at 18 statements a batch. #1528's dirtied-day rebuild and `metric_block_dirty` are gone.

A sample older than its series' window is now dropped before it lands, with one warning line a series
giving its count and age, and the rest of its batch lands. The trend recompute of a dirtied hour reads
a folded day from its block. The raw row still lands beside the merge until #1530, so
`VerifyMetricBlocks` still holds every block to its raw rows. ADR-1529 records where this differs
from the issue and from ADR-1528: a lock of the block's own in place of the trend lock, and no
re-spanning of a merged day.

One batch of 120 late samples into an hour block, a day block and a week block writes each block
once, 34,264 bytes of WAL (285 a sample) against 30,368 for the same batch landing raw only, so the
three rewrites cost 3,896 bytes (`TestLateSamplesRewriteTheirBlockOnce`).
