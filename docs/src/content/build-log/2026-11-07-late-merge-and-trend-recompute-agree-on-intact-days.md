---
title: "The late merge and the trend recompute agree on when a block covers a sample (#1546)"
date: 2026-11-07
refs: [1546, 1529, 1530]
---

With the window cutoff inside a folded day, a late sample of a still-intact hour was kept, skipped by
the merge (its day was no longer intact) and then left out of the hour's trend row, which the
recompute read from the day's block alone: the trend undercounted, and trends are kept for
`trend_days`. The recompute now reads a block only for a day that passes `blockIntact`, the merge's
own predicate, and reads the hour's raw rows otherwise. ADR-1529 names the shared predicate.

A late batch's wait for the block lock is also bounded now: two seconds, for that lock's statement
only. Behind a fold on another server the batch had held the trend lock shared while it waited, which
queued a trend compaction's exclusive request and every insert behind it. A batch that times out
fails whole and is redelivered.
