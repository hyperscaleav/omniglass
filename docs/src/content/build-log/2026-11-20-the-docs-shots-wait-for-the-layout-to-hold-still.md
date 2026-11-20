---
title: "The docs shots wait for the layout to hold still (#2168)"
date: 2026-11-20
refs: [2168]
---

The freshness gate failed on branches that touched no console code, each time on a different shot
(`users` by 5 px, `platform-health-connections` by 0.079%, `platform-health-outpost-worklist` by 4.4%)
and passed on a rerun. The platform-health shots hover a control that scrolls a table and opens a
tooltip, and the capture fired a fixed 1.2s after the last step, so a slow runner could photograph the
scroll or the tooltip's placement part way.

The capture now calls `settleLayout` before it shoots: every scroll offset, the document's size, any
open tooltip's box and the count of running animations must be identical for 30 consecutive frames,
and the fonts loaded, else the capture fails naming what kept moving. The tolerance stays zero. The
inbox badge (#2196) was a third cause and is fixed separately. The `users` 5 px region was not
isolated without a lab run; if it recurs, the capture output now rules out a layout still moving.

One more shot alternated between two exact renders for a reason the layout never showed: the lab's
outpost-silent alarm raises partway through a capture. `platform-health-outposts` read a healthy
liveness pill before it and an outage pill after, so it now waits (60 s, failing with a message
naming the alarm) for the outage pill. The inbox does not reliably receive the alarm's entry, so
`notifications-inbox` does not wait for it: its masked twin covers the whole content region, the same
with or without the entry, and the shot keeps its empty-inbox alt text.
