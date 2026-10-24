---
title: "Allocation tests the name it would mint, rather than reading the ordinal it stored"
date: 2026-08-10
status: "Accepted"
summary: "The generated ordinal becomes a stored nullable `component.ordinal`, but sibling allocation does **not** read it: it reads sibling NAMES and returns the lowest ordinal whose MINTED name is free. An operator can hold a generated-shaped name with no ordinal, and the unique index is on the name, so an ordinal-only allocator would remint a taken name as a `23505`. Minting candidates instead of parsing siblings is what makes a stem-less name (a floor called `1`) possible, not the column; the column is what every reader DOWNSTREAM of allocation consumes. No scoped-unique index on the ordinal"
---

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** The number a generated name is minted from becomes a **stored, nullable**
  `component.ordinal`, written in the same advisory-locked transaction that already computed it.
  Nullable is the load-bearing part: a name an operator typed, and a name an operator renamed
  (`:rename` clears the column in the same statement it clears `name_generated`), have no ordinal the
  platform owns, and absent is how that is written down.

  **Sibling allocation does not read that column.** It reads sibling **names** and returns the lowest
  ordinal whose **minted** name no sibling in the placement bucket already holds. One pure function
  (`mintName`) owns the name's shape, and the allocator asks it for candidates instead of picking
  siblings apart.
- **Decision (no scoped-unique index):** The ordinal gets no unique constraint of its own, only a
  `>= 1` domain check. A "one ordinal per (bucket, stem)" index cannot be expressed, since the stem is
  resolved from the `component_type` chain rather than stored, and it would be redundant if it could:
  for a platform-named row the name **is** the stem and the ordinal formatted together, so two rows
  colliding on both already collide on the name, which the scoped-name unique indexes
  ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)'s
  placement buckets) refuse. A redundant constraint would add a second `23505` to map and a second way
  for a race to surface as a 500.
- **Context:** [#657](https://github.com/hyperscaleav/omniglass/issues/657) proposed that storing the
  ordinal would let sibling allocation "stop being a string-prefix scan and become a lookup keyed on
  type and placement". Building it exposed two problems with reading the column, and one thing the
  column was being credited with that it does not actually do.

  **An ordinal-only allocator is unsound.** The rows holding a NAME are not the rows holding an
  ordinal. An operator can type `display-1` by hand: that row owns the name and, correctly, no
  ordinal. An allocator consulting stored ordinals sees an empty set, picks 1, mints `display-1`, and
  hits the scoped-name unique index. The generator deliberately takes an advisory lock rather than
  retrying a `23505` (retry was rejected when the generator landed, since the abort takes the whole
  transaction with it), so that collision is a 500 on an ordinary create. Mutating the allocator to
  read only rows with a stored ordinal reproduces it exactly, and the test that catches it is now
  part of the suite.

  **Keying on the type would be worse than keying on the stem.** A stem is inherited, so two sibling
  component types can resolve the same one; giving each its own ordinal space would have both mint
  `display-1`. The uniqueness that matters is the name's, so the space that matters is the name's.

  **The column is not what unblocks a stem-less name.** The prefix scan could not count a stem-less
  sibling because its filter was a bare `-` that no name matched, which is a property of *parsing*,
  not of *storage*. Inverting the loop fixes that on its own: `mintName("", 1)` is `1`, and the
  allocator tests it like any other candidate. So a floor whose ordinal genuinely is its name
  ([#657](https://github.com/hyperscaleav/omniglass/issues/657) slice 7) is now expressible, and it is
  asserted at the storage layer against a real database ahead of its first caller.

  What the stored ordinal does buy is everything **downstream** of allocation, which is what the rest
  of the epic needs: the bare render reads a fact instead of re-deriving one from the string it is
  about to replace (finishing what
  [#654](https://github.com/hyperscaleav/omniglass/issues/654) left half-done, and making its
  never-restamp-an-operator's-name guarantee structural rather than defensive), a label rule can read
  `.Ordinal` without parsing it back out, and a recompute-and-compare invariant test can prove no
  stored ordinal has drifted from what allocation would produce. It also generalizes: when a name
  comes from an operator-editable rule rather than `<stem>-<n>`, the allocator changes its mint and
  nothing else, where a parser would have needed a matching second implementation.
- **Tracked under** [#681](https://github.com/hyperscaleav/omniglass/issues/681), the first slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).
