---
title: "A location-scoped reader is paged for components placed in its location (#2554)"
date: 2026-11-23
refs: [2554, 1981, 1498]
---

A principal whose only component read reach is a location grant could list a component placed in
its location but was never chosen as a notification recipient for it: the queue-time recipient
filter judged read by `Set.AdmitsChain`, which ignores the cross-tier `LocationIDs` and fails
closed (ADR-1498, point 4).

`readersOf` now also reads the component's effective placement in one statement
(`componentPlaceChain`, mirroring `placedCTEs`: its own location, else its primary system's, plus
that of any placed ancestor) and admits a reader when the new pure `Set.AdmitsPlacement` finds one
of the set's location roots in it. A person is paged for exactly the components they can read.
Regression tests: `TestALocationScopedReaderIsPagedForAComponentPlacedInItsLocation` and
`TestAdmitsPlacementJudgesTheLocationChain`.
