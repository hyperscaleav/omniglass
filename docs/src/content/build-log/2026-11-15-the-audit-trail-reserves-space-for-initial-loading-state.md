---
title: "The audit trail reserves space for its first page (#1461)"
date: 2026-11-15
refs: [1461]
---

The probe's vitals pass measured 0.755 cumulative layout shift on `/audit`, far over the 0.1 budget: the table
rendered a one-line "Loading…" row (and, before the mount, "No events yet."), then a hundred rows landed and
moved everything below them.

`FlatList` gains an opt-in `loadingRows`: while `loading` is true that many skeleton rows replace the
"Loading…" line, inside the same table, so the header and filters stay mounted and the loaded rows land in
place. `/audit` opts in with a screenful of rows, starts in the loading state so even the first paint is
reserved, and gives its "Load older" button a fixed minimum width so the footer does not move when the label
flips to "Loading…". The audit data, filters and paging are unchanged.
