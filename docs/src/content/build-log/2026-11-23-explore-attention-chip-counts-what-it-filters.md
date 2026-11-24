---
title: "Explore's need-attention chip counts what it filters (#2438)"
date: 2026-11-23
refs: [2438, 2302]
---

The chip said "2 need attention" and pressing it listed five rows, while Home said three open alarms.
One pure function (`lib/attention`) now counts the systems and components whose verdicts need
attention over the rows the attention filter leaves, and the chip's number, its words ("2 systems,
3 components need attention") and Home's new Need attention card all read it. It takes verdicts as
data, so a health rule that changes server-side moves every figure at once.

To make the rows countable, the filter list gives a place holding several systems no health of its
own and a row per system, and lists a shared component once. The pressed chip keeps its dot; every
outline name goes through one renderer (no data font); the Health column packs counts left; a
place's "2 systems" moved from Standard to Detail, which wraps with a tooltip; and the filter
narrows as you type, with Enter pinning the chip. Matches keep their path, which carries each
match's ancestors; the tree-shaped version of that belongs to #2302.
