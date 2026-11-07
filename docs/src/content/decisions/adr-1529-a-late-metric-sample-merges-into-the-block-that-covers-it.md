---
title: "A late metric sample merges into the block that covers it"
date: 2026-11-07
status: Accepted
summary: "A late observed sample for a day the block fold has passed merges into its covering block (the hour block of its hour, the day block of its day, else its ISO week's block once the week has folded) in its own batch's transaction, each block rewritten once a batch from the block alone, never from the raw rows. The fold and the merge are ordered by a block advisory lock of their own, which only late batches take, not by the trend lock. The block keeps the span it was folded at. The raw row still lands until #1530. A sample older than its series' window is dropped and logged, the rest of its batch landing. A dirtied trend hour of a folded day recomputes from its block while the day is intact (the merge's own test, `blockIntact`), else from its raw rows. A late batch waits for a fold with no timeout. #1528's dirtied-day rebuild is removed."
refs: [1529, 1528, 1265, 1276, 1530, 1546]
---

- **Date:** 2026-11-07 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1529, amended by #1546 in decisions 1, 7, 8 and 9; amends [ADR-1528](#adr-1528-observed-metric-samples-fold-into-lossless-blocks-sized-by-their-sample-count)'s decision 9, the dirtied-day rebuild.)
  1. **The covering block.** A late observed sample (one landing in an hour the trend compaction has
     announced, [ADR-1276](#adr-1276-hourly-trends-close-an-hour-a-day-after-it-ends-and-hold-back-the-prune))
     whose UTC day the day fold has passed for its series, and which is still inside its series' window,
     is covered by: the hour block of its hour when the day was cut into hour blocks (a new hour block
     when its hour had none); else the day's block; else, once the week fold has passed the series' ISO
     week, the week block (a new one when the week had none). A sample of a day the fold has not
     reached, or of a week the week fold has not, is covered by nothing and lands as a raw row, which
     the fold reads. "Still inside its series' window" is `blockIntact`: the day's start is inside the
     window (`coalesce(retention_days, retention.metric_days)` back from now). It is the one predicate
     for whether a block covers a sample, and decision 7's recompute reads it too.
  2. **One write a block a batch, from the block.** After its raw rows land, the batch merges its late
     samples in one `insert ... on conflict do update` grouped by covering block and written in key
     order. The update unnests the block as it stands and the samples it gains, ordered by offset with
     the block's own samples first at an equal instant, then landing order, and writes the block and
     its `n` once. It never reads a raw row. Two batches merging into one block queue on its row, and
     the second merges into what the first committed.
  3. **The block lock orders the merge against the folds.** The issue proposed the trend lock. The
     folds never take it, and taking it exclusive in a fold would stall every metric insert for the
     fold's transaction. So the merge takes a lock of its own, `metric_block:fold`, shared, in a
     statement before the merge. Each fold transaction takes it exclusive after it locks the mark row,
     and holds it to its commit. The merge's statement then reads the marks of every fold that committed
     before it, and no fold commits past those marks before the batch does. A fold that waits for the
     batch reads its raw rows after it commits. An on-time batch takes no part: it merges nothing and
     never takes the lock.
  4. **The span is not revisited.** ADR-1528 decision 9 let a rebuilt day cross 170 samples and leave
     the week block for a day block of its own. A merged block keeps the span its day was folded at,
     however many late samples it gains, so a merge touches one block and never moves samples between
     spans. A day the replays carry across a threshold costs a little compression, not correctness.
  5. **The raw row still lands.** Until #1530 drops raw days early, raw rows stay authoritative for
     every reader, so a late sample lands raw and merges. `VerifyMetricBlocks` still holds each block
     to its raw rows. Its one known difference is two concurrent batches reporting one series at the
     same millisecond: they take block order by commit, not by raw id.
  6. **Too old is dropped and logged.** An observed sample older than its series' window (its type's
     `retention_days`, else `retention.metric_days`) is dropped before it lands, with one warning line
     a series naming its owner, key and instance, how many samples it dropped and the oldest's age. The
     rest of the batch lands and is acknowledged. The prune would take such a row at once, and its day's
     block is already being pruned.
  7. **A dirtied hour recomputes from its block.** The trend recompute reads a dirtied hour of a folded
     day from the block that covers it (decision 1's choice, read from the blocks that exist), and any
     other hour from its raw rows, so it is right once #1530 drops the raw rows. It reads a block only
     where the merge would have written to it: the day passes `blockIntact` (decision 1), the same SQL
     predicate in both. The recompute keeps an hour by the hour's own start, which is later than its
     day's, so with the window cutoff inside a folded day a late sample of a still-intact hour is
     kept, skipped by the merge (the day is no longer intact) and landed raw only. Reading that hour
     from the day's block would leave the sample out of a trend row that is kept for `trend_days`, so
     such an hour reads its raw rows, which exist until #1530. Each hour reads exactly one source.
     When #1530 drops raw rows early it must keep this case covered.
  8. **The rebuild is removed.** `metric_block_dirty`, the rebuild pass and `BlockCompaction.Rebuilt`
     go. An on-time batch is unchanged at 18 statements. A batch with a late sample (one landing in an
     announced hour) adds 3: the settings read, the block lock and the merge. A batch carrying a sample
     a day old or more adds the window check, and the settings read when nothing else has made it, so
     2 alone and 4 when it is also late (the settings read is made once).
  9. **A late batch waits for a fold, with no timeout.** The batch holds the trend lock shared (the
     insert read the announced mark under it) while it waits for the block lock. On a multi-server
     install a fold chunk on one server holds the block lock exclusive, so a batch waiting behind it
     holds the trend lock shared across the wait; a trend compaction on another server asking for the
     trend lock exclusive queues behind the batch, and every new insert queues behind that request,
     until the fold commits or `trendDrainTimeout` (5 s) fails the compaction. The stall is bounded
     there, and what it fails is the compaction (the next pass retries it), not the batch. It cannot
     occur on one server: `retentionPass` runs the trend fold and the block fold one after the other,
     so no fold holds the block lock while a compaction waits. The merge sets no `lock_timeout`
     (`TestMergeLateBlocksSetsNoLockTimeout`): the consumer Terms a message after
     `maxTelemetryDeliveries` (5) redeliveries, about 15 s, so any fold chunk longer than that would
     drop the batch, its on-time rows included. A late batch waits for the fold to commit and lands
     once (`TestALateBatchWaitsOutAFoldWithoutBeingGivenUp` holds the lock for 21 s).
- **Context:** #1528 rebuilt a dirtied day from its raw rows, which #1530 is about to drop early. The
  rebuild ran under the trend lock taken exclusive, which stalled the ingest while it read whole days.
  A merge costs the batch that carries the late samples, in proportion to the blocks it touches.
  Measured in `TestLateSamplesRewriteTheirBlockOnce`, one batch of 120 late samples (40 each into an
  hour block of 170, a day block of 170 and a week block of 5) writes 34,264 bytes of WAL, 285 a
  sample, against 30,368 for the same batch landing raw only into a day not yet folded. The merge's
  three block rewrites cost 3,896 bytes.
- **Not chosen:**
  - **The trend lock, as the issue proposed.** See decision 3.
  - **A lock timeout on the merge.** A batch that gave up would be redelivered at most four times in
    about 15 s, then Term'd; a fold chunk can run longer. See decision 9.
  - **Merging without the raw row now.** Every reader still reads raw rows until #1530.
  - **Re-spanning a merged day.** It would rewrite a day's blocks and its week's block on one sample,
    which is ADR-1528's rebuild again.
