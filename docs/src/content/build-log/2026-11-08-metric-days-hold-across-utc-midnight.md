---
title: "Metric days, the drop log and the retention test hold across UTC midnight (#1567)"
date: 2026-11-08
refs: [1567, 1527]
---

A run on main crossed UTC midnight and three tests failed. `TestNodeProbeReadsServeOnlyObserved`
planted a row an hour back, in yesterday, and the template its binary built after midnight had no
partition for it (`no partition of relation "metric" found for row`, 23514).
`TestATooOldSampleIsDroppedAndCounted` saw its drop warning twice: the batch missed yesterday's day,
was retried whole, and logged in each attempt. The bus retention test seeded its block marks on a
Sunday, at that week's Monday, and two days a pass could not catch up (`Behind: [metric_block
compaction]`).

The roll now keeps yesterday through three days ahead, and the test template is rolled when it is
built. The drop warning goes out when its batch commits. A new `storagetest.PinClock` pins a test
database's clock, and `AtEachMidnight` runs the three tests, and the roll's, at the real time and 30
seconds either side of midnight. `TestMidnightAloneLeavesNoFoldBehind` states when the block fold
reads `Behind` across a midnight
([ADR-1567](/architecture/decisions/#adr-1567-the-metric-roll-keeps-yesterday-and-a-retried-batch-logs-its-drop-once)).
