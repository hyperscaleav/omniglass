---
title: "A location move recomputes both ancestor chains"
date: 2026-08-09
status: "Accepted"
summary: "`MoveLocation` recomputes health over both ancestor chains (joined and left) inside its own transaction, the second and last member of the exception class ADR-0088 carved out for a system's relocate; one named row per side seeds the recursive ancestry walk the query already performs, and a no-op move recomputes nothing"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [health](/architecture/health/),
  [API](/architecture/api/)
- **Decision:** `MoveLocation` recomputes health when it changes a location's `parent`, inside the
  transaction the move already opens, over BOTH ancestor chains: the one the location joined and the
  one it left. This is the second and last member of the exception class
  [ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb) carved
  out for a system's relocate, not a reversal of its "a placement move never recomputes health"
  ruling: `:move` recomputes exactly where the rollup genuinely depends on the placement being
  changed, which is a system's `location` and a location's `parent`, and nowhere else (a component's
  or a system's `parent` stays health-inert, and so does a component's `location`, since a
  component's own verdict is purely its active alarms per
  [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability)).
  The trigger names ONE row per side, the moved location and the parent it left, not a walked chain:
  `locationsOver` already takes its named locations as the seed of a recursive ancestry CTE, so each
  named row carries every ancestor above it, and walking either chain in Go first would reimplement
  in a second place the walk the query performs. Resolving the old chain after the write is equally
  safe, because the only parent edge the write touches is the moved row's own, so the old parent's
  ancestry reads the same before and after. A `:move` that changes no parent (the documented no-op)
  recomputes nothing, the same guard the system relocate applies.
- **Context:** [#642](https://github.com/hyperscaleav/omniglass/issues/642), filed by ADR-0088's own
  review round and closed here. The gap predates the `:move` split: `UpdateLocation`'s old reparent
  branch never recomputed either, so nothing has ever recorded this edge. It matters because a
  location's verdict is the one rollup that genuinely depends on where the row sits: `locationVerdict`
  folds every system in the location's own subtree, walked downward by a recursive CTE, so a location
  with placed descendants moving to a new parent really does change what its old and new ancestors
  should read. Left open, an operator reorganizing a campus would see the abandoned branch frozen at
  the verdict of a room that is no longer in it, and the new branch reading healthy over a room that
  is. The reads never lied (both health reads compute the verdict they serve), so the cost was
  confined to the recorded history, which is exactly the "a missing trigger is a hole in the history"
  cost the health page enumerates its trigger list to avoid.
