---
title: "A position is a slot you can address"
date: 2026-08-08
refs: [626]
form: bullet
---

- **A position is a slot you can address** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `dfda3ab`, `4c6012e`). An assignment carries a 1-based position within its role, ordered by
  creation and never renumbered on its own: unassigning leaves a gap, and the next assignment
  refills the lowest free slot rather than growing past capacity. Both resolved reads
  (`EffectiveRoles` and the health rollup) report a role's occupants in position order via a
  correlated subquery rather than a `GROUP BY` plus `DISTINCT`, since the roles query already
  joins types and products and a naive distinct ordering would have hidden that cartesian product
  instead of just losing order. Position uniqueness is a deferrable constraint, not a unique
  index, added after a one-time backfill: a plain index is checked per updated tuple, which would
  raise the moment a single-statement swap lands the first row on the other's slot.
  `SwapPositions` defers it for the rest of its transaction before exchanging two occupants in one
  UPDATE (`POST /systems/{name}/roles/{role}:swapPositions`); it and `AssignRole` both take
  an advisory lock on the `(system, role)` pair, so two concurrent assigns cannot compute the same
  next-free position. The health rollup gains `short` and `spare`, the occupancy-aware
  counterparts of the roles read's health-blind `understaffed` (a role can read fully staffed
  there and still short here, if what it has is down). A same-slice follow-up (`4c6012e`) found
  capacity enforcement had been emergent from the position collision, mislabeling the refusal
  once positions were contiguous and silently overfilling past the declared cap once a gap
  existed below it; added an explicit pre-check (`CapacityFullShortfall`, 409) beside the
  existing double-staffing one, skipped when the component already holds the role so a full
  role's existing occupants stay idempotent, and renamed the swap body fields from `{a, b}` to
  `{position, with}` (the former generated unusable CLI flags).
