---
title: "The late merge and the trend recompute agree on when a block covers a sample (#1546)"
date: 2026-11-07
refs: [1546, 1529, 1530]
---

With the window cutoff inside a folded day, a late sample of a still-intact hour was kept, skipped by
the merge (its day was no longer intact) and then left out of the hour's trend row, which the
recompute read from the day's block alone: the trend undercounted, and trends are kept for
`trend_days`. The recompute now reads a block only for a day that passes `blockIntact`, the merge's
own predicate, and reads the hour's raw rows otherwise, so each hour reads exactly one source.
ADR-1529 names the shared predicate. The tests pin the database clock, so the cutoff sits exactly on a
day's start, on an hour's start or inside an hour, for a type's own window and for the lane's.

The late merge still waits for a fold with no timeout: the consumer Terms a batch after five
deliveries (about 15 s), so a timeout shorter than a fold chunk would drop the batch, on-time rows
included. ADR-1529 records the bound instead: the stall a waiting batch can cause a trend compaction
on another server ends at `trendDrainTimeout` and fails the compaction, not the batch. It cannot occur
on one server, which runs the folds in order. A late batch adds 3 statements (settings read, block
lock, merge) and a batch carrying an old sample adds the window check; an on-time batch stays at 18.
