---
title: "A late batch keeps its slot and a widened window keeps its sample (#1560)"
date: 2026-11-15
refs: [1560, 1559, 1546, 1529, 1530]
---

Two advisories from the review of #1559. A late batch waiting on the block fold lock sat unacked past
the consumer's 30 s AckWait, so the stream redelivered it, spending a delivery of its 5 and freeing the
single ack-pending slot, which let another server land the publisher's next batch first. The consumer
now sends `InProgress` every `telemetryAckWait/3` for as long as the landing runs and stops when it
returns, so the batch is delivered once and the next lands after it. The ADR's bound was wrong too: a
batch left unacked is given up after 5 x 30 s = 150 s, not about 15 s. `TestALateBatchBehindAFoldIsHeldByItsHeartbeat`
(real Postgres and JetStream, a shortened AckWait, the lock held past several) saw 8 landings without
the heartbeat and 2 with it, and the unit tests hold the beat inside the AckWait on both lanes.

The second: a late sample with the window cutoff inside a folded day lands raw only, and the trend
recompute, a later transaction, trusted the window as it then was. An operator who widened it in
between made the day intact and the recompute read the hour from a block that lacks the sample. The
merge now sets `metric_trend_dirty.raw_only` on the hour of each sample it skips from a folded day, and
the recompute reads such an hour from its raw rows. `TestAWindowWidenedAfterTheMergeLeavesTheLateSampleInItsTrend`
failed before the change on each span and passes after. Ingest stays at 18 statements per on-time
batch and the late batch statement counts are unchanged. See ADR-1529 decisions 7 and 9.
