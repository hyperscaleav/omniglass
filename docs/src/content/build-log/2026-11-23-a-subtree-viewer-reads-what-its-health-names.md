---
title: "A subtree viewer reads the systems its location health names (#1774)"
date: 2026-11-23
refs: [1774, 1498, 2487]
---

The synthetic user found `viewer-hq` (the `viewer` role on the hq subtree) shown a room's system in
the location health roll-up while `/systems` and `/components` listed nothing and the system itself
404ed. The cause: location health resolves on the location tier and names every system beneath the
location, while systems and components were scoped by their own tier alone (#714), so a location
grant reached none of them. The read half of #1498 (PR #1990) closed that by admitting a row by its
placement under a subtree location grant, but it was only driven with the `deploy` shape.

This slice adds no fix: `TestASubtreeViewerReadsTheSystemsAndComponentsItsHealthNames` in
`internal/api` drives the viewer shape over the wire, asserting the list and get (by name and
dotted address) are exactly the hq rows, nothing in another campus reads, and the location health,
the system list and the bulk system health name the same systems. It fails on the commit before
#1990 with the ticket's symptoms. A `self` location grant still sees system names in its health
drill-down it cannot open; that is #2487.
