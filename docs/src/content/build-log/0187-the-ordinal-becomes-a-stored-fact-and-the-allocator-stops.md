---
title: "The ordinal becomes a stored fact, and the allocator stops parsing names"
date: 2026-08-11
refs: []
form: bullet
---

- **The ordinal becomes a stored fact, and the allocator stops parsing names**
  ([#681](https://github.com/hyperscaleav/omniglass/issues/681), the first slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0097](/architecture/decisions/#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored)).
  The number a generated component name is minted from is written down (`component.ordinal`,
  nullable) instead of being formatted into the name and dropped. `NULL` is the honest state for a
  row the platform did not name, so `:rename` clears it in the same statement it clears
  `name_generated`, and the bare render now reads the column rather than re-deriving an ordinal from
  the string it is about to replace. That finishes what #654 left half-done and turns its guarantee
  structural: a component an operator called `rack-3` has no number to compact, so there is no rule
  left to get wrong, and a hand-typed `display-1`, which the stem match still compacted, is left
  alone too.

  The slice ran first because it carried the epic's riskiest assumption, and the assumption turned
  out to be half right. Storing the ordinal is necessary for everything downstream (the render, a
  label rule's `.Ordinal`, the recompute-and-compare invariant), but it is **not** what unblocks a
  stem-less name, and allocation must not read it. The prefix scan could not count a stem-less
  sibling because its filter was a bare `-` that no name matched, which is a property of parsing
  rather than of storage; inverting the loop to test the name it would **mint** against the bucket
  fixes that on its own, and `mintName("", 1)` is `1`. Reading the column instead would have been
  actively wrong: an operator can type `display-1` by hand, holding the name while owning no
  ordinal, and an allocator that consulted only recorded ordinals would remint it into the
  scoped-name unique index as a `23505` the transaction cannot recover from. Mutating the allocator
  to read only rows with a stored ordinal reproduces that failure exactly, which is now a test.
  Every existing allocator unit test passes unchanged, because the two formulations agree on every
  answer they can both express.

  Six mutations back the assertions: an ordinals-only sibling read, a `:rename` that keeps its
  ordinal, a `:move` that does not re-record it, `max+1` in place of lowest-free, a render that
  ignores the column, and a backfill that fabricates a number for a row it cannot read one off. Two
  of them exposed that the recompute-and-compare invariant was passing for the wrong reason (its
  estate had no row that stayed renamed, and its one move never left its placement bucket), so the
  estate grew both cases before either mutation counted as caught.
