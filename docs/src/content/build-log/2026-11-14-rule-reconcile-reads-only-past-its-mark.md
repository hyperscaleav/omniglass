---
title: "The rule reconcile reads a finished day's index, not its heap (#1929)"
date: 2026-11-14
refs: [1929, 1896]
---

Each minute the event rule reconcile read its marks (`max(id)` past the lower mark) and the series
touched past it, and both walked every day partition of `metric`: a finished day's block ranges
were never summarized, so each read as a match and its heap was scanned whole, and the touched
scan probed each watched series' whole day in its btree. A migration now sets `autosummarize` on
each existing day's own block range indexes, and the partition roll makes every new day's with it
before the attach; each retention pass, after its drop and on a deadline of its own, summarizes every
finished day's ranges, the last one a day never fills included, naming a day that fails and going on
past it; and the touched scan reads the ids past the mark first, on their own, so each day is reached
through its id index ([ADR-1929](/architecture/decisions/#adr-1929-a-finished-day-is-summarized-and-the-reconcile-keeps-its-id-marks)). An EXPLAIN test over a filled finished day shows both reads
fall from 316 and 536 of its pages to a few index pages, and a slow sample stamped hours into that old
day, written past the mark around the evaluator, still fires its rule once over three passes. The
marks still read the newest committed id and a pass still reads from the mark of the pass before
last, so the guarantee for a batch in flight is unchanged.
