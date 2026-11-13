---
title: "The label scan keeps its for update, with a witness (#731)"
date: 2026-11-12
refs: [731, 709, 1805]
---

#709 found that `FOR UPDATE` inside a resolving read serialises writers without refreshing what the
read resolved, and fixed the registry fork. The label recompute's row scan has the same shape, with
correlated subqueries for the location, product, standard and parent names around each row, so #731
asked whether a label could be composed from names its scan saw before a lock wait.

It cannot, and the code now says why beside the `for update`. A label reads the locked row's own
columns, which the re-read after the wait refreshes, and facts resolved by separate statements after
the scan. No label key reads a scan subquery column, and a unit test now pins that. A contention
test forces the interleaving rather than racing for it: a held row lock stops a bulk recompute in its
scan, a location relabel queues its cascade behind the operation lock, and a product rename commits
in the window. A control statement shaped like the scan reads the pre-rename product label every
round, which proves the window is real. The recompute reads the new one. Rendering `ProductName`
from the scan's subquery fails the test at round 0.

Tracing the writers turned up a different window, filed as #1805: a component moved into a location
while that location's label is edited keeps the old location label, because the move's own stamp
reads the location unlocked and the cascade's scan never sees the moved-in row. The forced
interleave reproduces it every time.
