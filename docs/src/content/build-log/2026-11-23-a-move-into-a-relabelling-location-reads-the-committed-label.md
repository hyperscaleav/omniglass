---
title: "A move into a location being relabelled reads the committed label (#2552)"
date: 2026-11-23
refs: [2552, 731]
---

A component moved into a location while that location's label was being edited kept a label naming
the old location label: the move's stamp read the uncommitted-edit ladder without a lock, and the
cascade's scan snapshot predated the mover. `MoveComponent` and `MoveSystem` now take the
`label/recompute` lock before writing when they set a location. Regression test:
`TestAComponentMovedIntoALocationDuringItsRelabelReadsTheCommittedLabel`, a forced interleave.
