---
title: "A node running a stale worklist reads as loss (#1868)"
date: 2026-11-13
refs: [1868, 1623, 1583, 1586]
---

On the corporate 3,000 soak the headquarters node's worklist reply grew to 1.17 MB compressed, past
the bus's 1 MiB limit, so the server refused every pull after the first. The node kept its startup
worklist, as designed, and every loss ratio read 0: its 8,850 tasks still matched the database, so
nothing had diverged yet. Any change for that node would never have reached it, and nothing said so.

The node now reports `worklist-pull-loss-ratio` on its node-self lane every report window: the pulls
refused or unreadable over the pulls made, with a window that made no pull carrying the last outcome,
so a node refused on every pull reads 1. The seeded `worklist-stale` rule opens a warning after two
minutes at 1 and clears on the first pull served. Platform health gains a Worklist column in the node
table, the ratio as a percent and red above 0. `omniglass lab measure` prints a worklist pull loss
row and gates a new exact family, `worklist_loss/<node>`, failing any node above 0 and failing as not
measured when the read found no node reporting.

Fitting the reply under the limit is #1583 and #1586; this makes any failing pull visible, whatever
its cause.
