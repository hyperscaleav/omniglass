---
title: "The metric lane's prune leaves a droppable day to the drop"
date: 2026-11-15
status: Accepted
summary: "The metric lane's chunked prune works only inside a day a longer type's sample holds; a day no such sample holds is left whole for the drop, which takes it once the day is wholly past the window. A sample with no window of its own is kept at least metric_days and leaves with its day, up to a day later. Typed prunes are unchanged. Amends ADR-1527 point 4 and ADR-1275 point 6's exact window."
refs: [1957, 1958, 1527, 1275, 1276]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1957, amending [ADR-1527](#adr-1527-the-metric-lane-is-partitioned-by-utc-day) point 4
  and the exact window of [ADR-1275](#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month)
  point 6.)
  1. **The lane's prune skips a day the drop will take.** Before it deletes in a day, the metric lane's
     chunked prune (the one for every type with no window of its own) asks the drop's own question
     of it, as the drop will first ask it: does the day hold a sample of a type whose
     `retention_days` window still reaches the day's end (for a day wholly past the cutoff) or the
     cutoff (for the day the cutoff sits in, whose end the drop reads once the day is past)? A day
     that does is pruned chunk by chunk as before. A day that does not is left whole, and the drop
     takes it once the day lies wholly before the window and the trend's compacted mark. The
     question is asked again on every call, so a day a longer type stops holding is left to the drop
     from then on.
  2. **A failed drop is retried at the catch-up cadence.** The drop is now the only way out of a day
     no type holds, so a pass whose drop failed names the lane behind and the next pass comes a
     minute later, not an hour.
  3. **The window is a floor, not an edge.** A sample of a type with no window of its own is kept at
     least `retention.metric_days` and leaves with its day, at most one day later. A sample whose
     series is gone (its owner deleted) leaves the same way, unless a held day keeps it for the
     prune. A type with a window of its own is still pruned to its window exactly, in every day.
  4. **Unchanged:** the trend horizon still caps both the prune and the drop
     ([ADR-1276](#adr-1276-hourly-trends-close-an-hour-a-day-after-it-ends-and-hold-back-the-prune)
     point 5), and a held day keeps every type's window exact inside it.
- **Context:** the prune ran in every day as the cutoff crossed it, so by the time a day was wholly
  past the window the prune had already deleted every row in it, and the drop only ever removed
  empty days. On the 3,000-system corporate soak that was about 7 to 9 million deletes an hour, each
  writing WAL and leaving a dead tuple for vacuum, for rows a drop removes for free. ADR-1527's "a
  day bounds that to one day past the window" held only for the drop; the prune never let it run.
- **Consequences:**
  - In a day no longer type holds, the lane's prune deletes nothing; the deletes it did there are
    gone, with their WAL and vacuum work. No shipped type has a window of its own, so on a stock
    install that is every day.
  - A type kept longer than the lane and sampled every day holds every day, and then the lane's
    rows in each are still deleted one by one, as before. Making that a drop too means rows of
    different windows no longer share a day's partition (#1958).
  - The lane's deleted count now reads about zero a pass. Removal shows as
    `metric-partition-dropped-count` instead, and a drop that stops taking days shows as a rising
    `metric-oldest-day-age`.
  - Reads clamped to a window are unchanged: the bucketed series read and a series read with a
    window both start at the window. A read of a series' newest sample with no window (the latest
    value behind calculations, rules, instance values and the platform view, and a series read with
    no window) can show a series that stopped reporting for up to a day longer than the window
    before it reads as no reading. That is accepted, not clamped: a series still measured always has
    a newer row, so only a stale one is affected, and by at most a day.
- **Not chosen:**
  - **Floor the lane's cutoff to the start of its day:** the same for a day no type holds, but a
    held day would keep the lane's rows up to a day past the window for no saving, since the prune
    works in that day either way.
  - **Drop a day at the cutoff and stop the lane's prune altogether:** a held day would keep every
    type's rows until the longest window passed, which is the month-era holding ADR-1527 rejected.
