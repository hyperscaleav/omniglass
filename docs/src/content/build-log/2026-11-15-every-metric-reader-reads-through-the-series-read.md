---
title: "Every metric reader reads through the series read (#1573)"
date: 2026-11-15
refs: [1573, 1530, 1572, 1574, 1546]
---

The second slice of #1530: the readers the first slice left on the raw `metric` table now read a
series' samples through the series read too. The calc window and current inputs, an event rule's
priming and stale check, the rule preview and its backtest replay, settlement's latest observation, the
effective values and the trend recompute answer the same before a day folds, after, and with the folded
week's raw rows gone, each proven against a recorded scenario on real Postgres. A series' current value
is its newest raw row unless a block holds a newer sample, so a late sample landing in a folded day no
longer reads as the newest. The trend recompute keeps #1546's one source an hour through the read's own
switch, a backtest takes a folded sample as arriving in time order, and a calc write runs at READ
COMMITTED like every other metric write.

`TestNoDirectMetricReads` now fails on any non-test read of the raw table outside the read, the folds
and an allowlist that says why for each entry. ADR-1530 records the slice. No raw row is dropped yet:
the pending table, the early drop and block retention are #1574.
