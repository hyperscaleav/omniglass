---
title: "A location move recomputes both ancestor chains"
date: 2026-08-09
refs: []
form: bullet
---

- **A location move recomputes both ancestor chains**
  ([#642](https://github.com/hyperscaleav/omniglass/issues/642),
  [ADR-0092](/architecture/decisions/#adr-0092-a-location-move-recomputes-both-ancestor-chains)).
  `MoveLocation` never recomputed health, so a location with placed descendants moving to a new parent
  left the branch it abandoned frozen at the verdict of a room no longer in it, and the branch it
  joined reading healthy over a room that was: the one rollup that genuinely depends on where a row
  sits, since `locationVerdict` folds every system in the location's own subtree. The gap predates the
  `:move` split (`UpdateLocation`'s reparent branch never recomputed either), which is why ADR-0088
  recorded it rather than closing it inside a task whose contract was to change no observable
  behavior. `recomputeMovedLocation` is the trigger, the location-tier twin of `recomputeMovedSystem`:
  it names ONE row per side, the moved location and the parent it left, because `locationsOver`
  already takes its named locations as the seed of a recursive ancestry walk, so each named row
  carries every ancestor above it and walking either chain in Go would reimplement the walk the query
  performs. Resolving the old side after the write is safe for a reason worth stating: the only parent
  edge the write touches is the moved row's own, so the old parent's ancestry reads the same before
  and after. The regression is red on both halves independently, proven by mutation as well as by the
  original RED: dropping the left-behind parent fails only the old chain's assertions, dropping the
  moved location fails only the new chain's, and each side is asserted two levels up so an
  implementation that named the two parents without walking above them fails too. The trigger sweep in
  `health_invariant_test.go` gains a location-move step for the transition-only invariant, which the
  missing trigger never violated (nothing written is never a duplicate), which is precisely why
  nothing turned red on this for as long as it stood.
