---
title: "Block compaction counts the days it skips; 3 days is the shortest lane that folds (#2144)"
date: 2026-11-23
refs: [2144, 1623]
---

At `metric_days 2` the fold wrote no block yet moved its mark on: a day becomes foldable 48 hours
after it starts (the day's close plus the trend's 24-hour late-data horizon), and the day stops being
intact at the same instant. `BlockCompaction.Skipped` now counts each series-day with at least
`BlockDayMin` samples that the fold passes after it left its window, and the housekeeping log warns
with it. The storage page names 3 days as the shortest window that folds. Widening the window so a
2-day lane folds would move the merge's and the series read's intact test (ADR-1529, ADR-1530), so it
is left to a decision of its own. Regression test: `TestTheShortestLaneThatFoldsIsThreeDays`.
