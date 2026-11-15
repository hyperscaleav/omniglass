---
title: "The metric prune leaves a droppable day to the drop (#1957)"
date: 2026-11-15
refs: [1957, 1527]
---

The metric lane's chunked prune deleted every sample as the window's cutoff crossed it, so by the time
a day was wholly past the window it was already empty and the partition drop only ever removed empty
days. On the 3,000-system corporate soak that was 7 to 9 million deletes an hour, with their WAL and
dead tuples. The lane's prune now asks of each day the drop's own question, whether a sample of a type
kept longer than the lane still holds it, and works only in a day that is held; a day nothing holds is
left whole and dropped once it is past the window. A sample with no window of its own is kept at least
`retention.metric_days` and leaves with its day, at most a day later; a type with a window of its own is
still pruned exactly. `TestPruneMetricSamplesLeaveADroppableDayToTheDrop` failed before the change
(the prune deleted 3 rows where 1 was in a held day) and passes after, and the drop then takes the
unheld days whole. The decision is ADR-1957.
