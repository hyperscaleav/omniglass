---
title: "The health recompute costs what it touches (#1562)"
date: 2026-11-07
refs: [1562, 1555]
---

Seeding the corporate scale plan row by row slowed as the fleet grew, and the health rollup was why.
Every `AssignRole` recomputes the four locations above its system (room, floor, building, campus), and
each of those used to fold every system in its subtree. The campus call covers the whole site, so at
1,000 systems each call took 5.1 ms, against 0.65 ms at 100.

A location now rolls up from its direct children's recorded verdicts (its systems and its child
locations), one latest row each by the owner indexes, after the recompute locks its chain and works
from the bottom up ([ADR-1562](/architecture/decisions/#adr-1562-a-location-rolls-up-from-its-childrens-recorded-verdicts)).
The verdicts and the recorded history are the same. The recompute per `AssignRole` went from 6.3 ms
to 3.8 ms at 100 systems and from 23.9 ms to 4.0 ms at 1,000, and seeding the corporate plan at
1,000 systems went from 2m37s to 48.6s on the same machine. A new test holds every recorded system
and location verdict to a full recompute from scratch after each kind of write.
