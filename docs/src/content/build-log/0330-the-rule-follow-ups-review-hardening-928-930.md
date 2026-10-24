---
title: "The rule follow-ups' review hardening (#928, #930)"
date: 2026-10-19
refs: [928, 930]
---

The review of the three follow-ups caught the reconcile pass costing more than it healed at the edges.
A restart read the whole sample history (twice, through the two-pass lag), and the property lane had no
per-series index, so each newest-sample lookup read a series' whole history: a fresh pass now starts a
bounded lookback from the newest sample, and `property_series_idx` (component, type, instance, ts desc)
replaces `property_component_idx`, the twin of the metric lane's `metric_owner_idx`, asserted by an
EXPLAIN test on both lanes. The pass ran inside the sweep's goroutine without a deadline, so a slow pass
held up sustains and settlements; it now has a loop of its own, under a deadline, cancelled on shutdown.
And one series whose correction kept failing pinned the mark, widening every later pass: the marks now
always advance, and a failed series is retried on its own a few times, then dropped with an error.

On the severity page, the name is judged on the shared entity name rule (the length cap and the uuid-shape refusal included, and only when an edit changes it), a save
refreshes the cached alarm, health and fleet views a rename or re-rank moves, and a level write now
locks the rules naming the level before the level itself, the same order an ingest takes, so a rename
concurrent with a batch raising at the level waits for it instead of deadlocking (a regression test
reproduces the deadlock on the old order).
