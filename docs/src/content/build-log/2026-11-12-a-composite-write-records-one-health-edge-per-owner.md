---
title: "A composite write records one health edge per owner (#915)"
date: 2026-11-12
refs: [915, 912, 1206]
---

Adding a planned or simulated system staffed its role slots one by one, recomputing health for the
whole location chain after each, so the system's history recorded `incomplete` and then `healthy`
milliseconds apart: an edge no reader could have observed, since the transaction is atomic, and N+1
chain passes holding the advisory locks for the whole composite. The create-system and assign-role
bodies now take an optional deferral that collects their owners, and the plan runs one recompute over
the deduplicated components, systems and locations before commit. The system and each location above
it record exactly one health edge, the final verdict, and the recompute cost no longer grows with the
slot count. Single writes pass no deferral and recompute inline, unchanged.
