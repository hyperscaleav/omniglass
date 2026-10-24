---
title: "A system names itself, and the only one of its kind carries no number"
date: 2026-08-11
refs: []
form: bullet
---

- **A system names itself, and the only one of its kind carries no number**
  ([#686](https://github.com/hyperscaleav/omniglass/issues/686), the sixth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0101](/architecture/decisions/#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests)).
  The pen and both verbs spread from component to system and location; only a system generates,
  because `location_type` carries no stem and the slice that gives it one is
  [#687](https://github.com/hyperscaleav/omniglass/issues/687). A location's `:resetName` therefore
  refuses with the missing fact named rather than reporting a reset it did not perform, which is also
  the test that flips the day the rule lands.

  D3 is ruled: suppress the first ordinal, accept the order dependence, and write the sequence down.
  A room's only boardroom is `boardroom` and the second is `boardroom-2`; deleting the bare one while
  the second survives frees the bare name for the next create, and `boardroom-1` never exists at any
  point. The suppression is a field on a `nameMint` VALUE rather than a change to the shared shape,
  because a component that suppressed at ordinal 1 would rename every generated component that
  already exists, and `pickOrdinal` now takes that same value. That second half is the one that would
  have bitten: a suppressing mint beside an allocator still testing `<stem>-<n>` disagrees on exactly
  ordinal 1, so the second create in a room mints a name the first already holds and dies on the
  scoped-name index. One value passed to both makes the disagreement unrepresentable rather than
  merely unlikely.

  The placement bucket became a value for the same reason. A system has three buckets and a location
  has two, so the location constructor takes no location id at all: pairing a location with the
  three-way shape is not a mistake to avoid at each call site, it is a value that cannot be built.

  The write paths were DERIVED rather than listed, the lesson slice 5 paid for. The mint reads two
  things, the type chain's stem and the placement bucket, which yields create, `:resetName`, both
  arms of `:move`, and the `system_type` half of a reclassify, and it settles two cases a list would
  have missed: un-classifying a platform-named system is refused by the generator itself rather than
  by a branch written for it, and a `stem` edited on a shared registry row is estate-bounded, so it
  does not cascade, which is ADR-0100's line applied to the name side. Both foreign keys agree
  (`ON DELETE RESTRICT`), so no placement moves without one of those acts.

  Review found two defects after the slice was first built, and both came out of suppression rather
  than out of the plumbing. A reclassify was gated on the `system_type` field being PRESENT rather
  than on the classification changing, and the console sends that key on every save so an unclassify
  can clear: an operator editing a label, with a lower ordinal freed by an earlier rename, would have
  had their system renamed under `system:update` with no rename asked for. And the allocation lock
  still carried the stem in its key, which was sound only while the mint was always `<stem>-<n>`:
  stem `wall` at ordinal 2 and stem `wall-2` at ordinal 1 are the same name, so two concurrent
  creates in one room took different locks and one would have died on the scoped-name index. The
  lock now guards the bucket, which is the only partition of the name space a mint cannot cross.

  The honest limit: the system bare render stays unwired although both halves of its substitution now
  exist. `RenderBare` stamps `<abbrev><ordinal>` whenever it is given both, so a suppressed name
  would print `brd1` for a room named `boardroom`, a digit on a physical label that appears nowhere
  in the name. That is a rendering decision, and it is recorded rather than quietly deferred. The
  console is untouched too: the create flows are slice 8's.
