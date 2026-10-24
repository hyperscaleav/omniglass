---
title: "A location type opts in to naming its own locations, and a rule change renames nothing"
date: 2026-08-11
refs: []
form: bullet
---

- **A location type opts in to naming its own locations, and a rule change renames nothing**
  ([#687](https://github.com/hyperscaleav/omniglass/issues/687), the seventh slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0102](/architecture/decisions/#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)).
  `location_type` gains a nullable `name_rule`, and its presence IS the opt-in: null means an
  operator names every location of that type. Of the four shipped place types only **floor** ships
  with one, and it ships positional, so a floor created with no name is called `1` and the next `2`,
  allocated among that building's own floors. A campus, a building and a room stay operator-named on
  purpose: `17c` is ground truth an operator holds, and generating one would be the platform
  guessing. The verb `:resetName` stops refusing on this tier, which is the test
  [#686](https://github.com/hyperscaleav/omniglass/issues/686) planted for exactly this slice to flip.

  The one design call: the rule is a **declaration**, `{"stem": "...", "bare_first": <bool>}`,
  decoded straight into the `nameMint` the previous slice made the one home of a generated name's
  shape, and deliberately not the `label_rule` template sitting beside it on the same table. A label
  that fails to render degrades to the next rung of the read ladder; a name has no next rung. It is
  `NOT NULL`, it is in a scoped-unique index, and it is what a runbook outside this system stores. So
  the promise #686's acceptance made and nobody had built, that a rule which would mint an illegal
  name is refused when the RULE is edited, is kept here by MINTING from the rule at edit time:
  ordinal 1 and a nine-digit ceiling bound the whole output space, so a rule legal at both ends is
  legal everywhere in it. A template's output could only have been sampled. The expressiveness would
  have bought nothing either: a location rule can read `Name` (circular, it is what the rule
  produces) and `TypeName`, whose slug is the stem.

  Two rulings the slice owed. **A rule change renames nothing**, and there is no name-side recompute
  verb: #685's preview-then-apply is the LABEL cascade, and the asymmetry is the point rather than a
  gap, since a bulk relabel is recoverable and a bulk rename breaks every reference stored outside
  this system. `:resetName` is the deliberate one-row-at-a-time way onto a new rule. And **a
  positional type permitted at root is legal**, allocating `1`, `2` across the estate: the bucket is
  the placement, never the placement and the type, so two positional types under one parent already
  share an ordinal space, and refusing at root would refuse the large instance of a rule that holds
  everywhere. Its real consequence is addressing, not allocation, and the dotted address already
  answers it (`boi.17c.1` resolves where `1` is ambiguous).

  The write paths were derived from the mint's two inputs again, and it paid twice. The reclassify
  guard is the classification CHANGING rather than the field being present, because the console sends
  `location_type` on every save: that is the live defect review caught on the system tier one slice
  ago, refused entry here rather than repeated. And `:move` re-mints only when the parent actually
  moves, which is narrower than the system tier's, where the same re-mint runs on a move that changes
  no bucket; nothing generated a location name before this slice, so there was no existing behavior
  to preserve and the narrow form is the correct one.

  `location` also gains the nullable `ordinal` the previous slice deliberately withheld from it,
  since the writer it was waiting for is this one, and the recompute-and-compare invariant now covers
  all three trees.
