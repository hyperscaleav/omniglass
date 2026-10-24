---
title: "The first of its stem in a bucket carries no ordinal, and the mint that says so is the one allocation tests"
date: 2026-08-10
status: "Accepted"
summary: "A generated **system** name suppresses the ordinal on the first of its stem in a placement bucket (`boardroom`, then `boardroom-2`), and the order dependence is accepted: deleting the bare one while the second survives frees the bare name again, and `boardroom-1` never exists. Suppression is a field on the MINT rather than a change to the shape (a component still reads `display-1`), and the ALLOCATOR takes that same mint, so a suppressing mint and a non-suppressing allocator cannot disagree on ordinal 1 and turn the second create into a `23505`. A placement bucket becomes a value per entity kind, so a location's two buckets cannot be written as a system's three, and the allocation lock loses the stem from its key, since two stems can now mint one name. The pen and both verbs spread to system and location; only a system generates, and a location's `:resetName` refuses with the missing fact named. No backfill: the default false is the right value for a row an operator already named. **Amended (#696, #691):** the component tier's two guards close too, its `:move` with the identical bucket comparison and its reclassify on the RESOLVED STEM rather than the classification id, because a component reaches its stem through a product and two products of one `component_type` mint the same name. **Amended (#706):** the system tier's matching residual closes the same way (two `system_type` rows inheriting one stem from a shared ancestor mint one name), so both tiers compare the resolved stem and the divergence between them is gone"
---

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision (epic D3, ordinal suppression):** A generated **system** name suppresses the ordinal on
  the first of its stem in a placement bucket: a room's only boardroom is `boardroom` and the second
  is `boardroom-2`. The order dependence this buys is accepted rather than mitigated, and it is
  worth stating in full rather than in the abstract. Allocation is lowest-free
  ([ADR-0097](#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored)
  preserved it), so **deleting `boardroom` while `boardroom-2` survives lets the next create take
  `boardroom` again**, and `boardroom-1` never exists at any point in that sequence. The same rule
  therefore yields different names for the same estate depending on the order it was built in. The
  operator-facing string is what this epic exists to improve, and a room called `boardroom-1` when
  there is only one of it is the defect the epic was filed about.
- **Decision (suppression is a property of the MINT, not of the shape function):** `mintName` became
  a `nameMint` value: a resolved stem plus a `bareFirst` flag, with the shape as a method on it. It
  is NOT a branch inside the shape function, and it is not a global change to the shape, because a
  component that suppressed at ordinal 1 would rename every generated component that already exists
  (`display-1` is the shape #681 has been minting) and would break the no-expected-value-changes
  rule this epic runs under. Components are counted things in a rack, where `display-1` beside
  `display-2` is what an operator writes on the label; a system is a room, and a room with one
  boardroom in it does not call it `boardroom-1`.
- **Decision (the allocator takes the mint, not the stem):** `pickOrdinal` takes the `nameMint` and
  tests `mint.name(n)` for each candidate. This is the load-bearing half. A mint that suppressed
  while the allocator still tested `<stem>-<n>` would disagree on exactly ordinal 1, so the first
  create would take `boardroom` and the second would test a free `boardroom-1`, mint `boardroom`,
  and hit the scoped-name unique index as a `23505` the transaction cannot recover from. Passing one
  value to both is what makes that disagreement unrepresentable rather than merely unlikely: there
  is no second spelling of the shape to keep in step.
- **Decision (where the choice comes from, and the seam for the next slice):** `bareFirst` is a
  field on the mint, resolved per entity kind today (`componentMint` false, `systemMint` true) and
  intended to be resolved per TYPE.
  [#687](https://github.com/hyperscaleav/omniglass/issues/687) gives `location_type` a nullable name
  rule, and a rule that produces a positional name fills this same field from a per-type fact: it
  consumes the seam rather than replacing it. A per-kind default is where the fact lives until a
  type carries one, not a competing mechanism. A stem-less mint ignores the flag outright, since
  suppressing the ordinal of a name that IS its ordinal (a floor called `1`) would leave nothing at
  all.
- **Decision (the allocation lock is keyed on the bucket, and the stem comes OUT of that key):**
  The advisory lock was keyed on the table, the stem and the bucket, which was sound only because
  the mint was always `<stem>-<n>`, making two stems' name spaces provably disjoint. Suppression
  ends that: stem `wall` at ordinal 2 and stem `wall-2` at ordinal 1 are the same name, and both
  stems pass the rule a stem is validated with. Keyed on the stem, those two concurrent creates in
  one bucket take different locks, read the same siblings, mint the same name, and the loser gets a
  `23505` on a create that supplied no name at all. So the lock now guards what the unique index
  guards, the **bucket**, because the bucket is the only partition of the name space a mint cannot
  cross. The cost is that two creates in one room serialize whatever they are classified as, which
  is the price of the shape being per-type rather than fixed.
- **Decision (a placement bucket is a value, not a pair of pointers):** The lock key and the sibling
  filter became one `nameScope` per entity kind, because the kinds do not agree on how many buckets
  there are. `component` and `system` have three (a parent, else a location, else unplaced) and
  `location` has TWO (a parent, else root), since a location carries no located-at column. The
  location constructor takes no location id, so pairing a location with the three-way shape is not a
  mistake to avoid at each call site, it is a value that cannot be constructed. The key carries the
  table too, so a system and a component sharing a parent uuid never serialize against each other.
- **Decision (the pen spreads to both trees, generation to one):** `name_generated` and both verbs
  (`:rename` freezes it false, `:resetName` returns it) land on **system and location**; only a
  system generates. `location_type` carries no stem to mint from, so `:resetName` on a location
  refuses with a typed error naming the missing fact (422) rather than reporting a reset that did
  not happen. The pen still ships on that tier ahead of its generator, and that is the point: a
  location an operator names today is already frozen when #687's rule arrives, where a pen added
  later would have to decide retroactively who had named every existing row.
- **Decision (no backfill, unlike the label pen):** The column defaults `false` and no one-time
  migration touches it, which is the argument the component column already records: every row that
  exists when the migration runs was named by an operator, so claiming the pen retroactively would
  let the first `:move` silently rename real estate. That is the opposite of
  [#682](https://github.com/hyperscaleav/omniglass/issues/682)'s label pen, which WAS backfilled,
  and the difference is the field rather than a change of mind: "no label" is a state the platform
  may safely claim, while a row always has a name and somebody typed it.
- **Decision (the system bare render stays unwired, and this is not an oversight):** Both halves of
  the compact render's substitution now exist for a system (`system_type.abbrev` from
  [ADR-0096](#adr-0096-the-system_type-name-returns-as-the-coarse-space-taxonomy), and a stored
  ordinal from this slice), and `attachSystemPaths` still passes neither. `RenderBare` substitutes
  whenever it is given both, with no shape check, which is what makes
  [#654](https://github.com/hyperscaleav/omniglass/issues/654)'s guarantee structural for a
  component. A suppressed name carries no digits while owning ordinal 1, so wiring it would print
  `brd1` for a room named `boardroom`: a number on a physical label that appears nowhere in the
  entity's name, the exact defect the ordinal column's own migration cites. Correcting it means
  teaching the render which mint produced the name, which is a rendering decision, not a naming one.
- **Consequence (the write paths were derived, not listed):** The acts that must re-mint a
  platform-owned system name are the ones that change an INPUT to the mint, and the mint reads
  exactly two things: the `system_type` chain's stem and the placement bucket. That yields create,
  `:resetName`, `:move` (both the relocate and the reparent arm, since a parent wins over a
  location) and the `system_type` half of a reclassify, and it also settles two cases a list would
  have missed. Un-classifying a platform-named system reaches the generator with no type and is
  REFUSED there rather than silently keeping the name and handing back the pen. And a `stem` edited
  on a shared `system_type` row is bounded by the estate rather than by a placement, so it does not
  cascade, which is
  [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)'s
  line applied to the name side. The database agrees: `system.location_id` and `system.parent_id`
  are both `ON DELETE RESTRICT`, so no placement changes without one of these acts running.

  The same derivation settles the shape of the reclassify guard, which review caught the code
  getting wrong. The trigger is the classification **changing**, not the `system_type` field being
  present in the patch: the console sends that key on every save so an unclassify can clear, so a
  presence test re-mints on an edit to the label alone, and with a lower ordinal freed by an earlier
  rename that re-mint MOVES the name (`boardroom-2` becomes `boardroom`) under `system:update`, with
  no rename requested and possibly no `system:rename` grant held. A patch that re-states the type
  changes neither input to the mint.

  **The epic's review pass found the same defect on `:move`, and it is fixed here rather than
  filed.** The move arm was gated on the row being platform-named alone, so a `:move` that re-stated
  the location the system already sat at, or supplied neither field (this verb's documented no-op),
  re-minted and moved the name by exactly the sequence above, under `system:move`. It was left wide
  deliberately, on the stated belief that narrowing it would move an existing expected value; that
  premise was false, since systems had only had generated names since this same unmerged branch. The
  guard is now the **bucket** changing, compared as a `nameScope` rather than as a pair of pointers,
  because a parent wins over a location: relocating a parented system leaves it in the same parent
  bucket and must not rename it either. No test expectation moved. `MoveComponent` has the identical
  wide shape, is reachable the same way, and is genuinely pre-existing, so it is left for its own
  issue rather than changed here.

  **Amended ([#696](https://github.com/hyperscaleav/omniglass/issues/696),
  [#691](https://github.com/hyperscaleav/omniglass/issues/691)):** both component-tier guards are
  now closed, and one of the two is spelled DIFFERENTLY here rather than copied. `MoveComponent`
  takes the identical `nameScope` bucket comparison, since the two kinds have the same three buckets
  and a parent wins over a location on both. The reclassify does not. A system reads its stem from a
  `system_type` chain, while a component reads it from a PRODUCT that points at a `component_type`
  chain, one hop further, so the value an operator picks is not the value the mint reads: two
  products classified under one type (or under two types inheriting one stem) mint identical names.
  Keying the component guard on the product id would therefore leave a real reclassify moving the
  name onto a freed lower ordinal, which is the defect this entry refused for the presence test
  wearing a third set of clothes. It compares the RESOLVED STEM instead, and `stemForProduct` is the
  first half of `generateNameForProduct` lifted out so a guard can ask what a name would be minted
  from without minting one. A destination with no stem at all is never equal to anything, so it
  still reaches the generator and is still refused there. The system tier kept the type-id
  comparison and the same residual with it (two `system_type` rows inheriting one stem), a real case
  filed as [#706](https://github.com/hyperscaleav/omniglass/issues/706) rather than fixed inside a
  component-tier slice: the two tiers agree on the RULE (re-mint when an input to the mint moves)
  and disagreed only on how far each had to walk to read one. **That divergence closes with #706:**
  the system reclassify compares the resolved stem too, `stemForSystemType` is the first half of
  `generateNameForSystemType` lifted out exactly as `stemForProduct` was, and `systemTypeStemMoved`
  is `productStemMoved`'s twin over the shorter walk. Both tiers now spell one rule, and each keeps
  its own path to a stem: a system's chain, a component's product then chain. An un-classify and a
  destination that resolves no stem are still never "the same stem", so both still reach the
  generator and are still refused there rather than by a branch in the guard. The claim that the component
  reclassify was "not reachable the same way today" survived checking and is narrower than it
  sounds: the console never sends `product` on a save (`web/src/pages/Components.tsx` edits the name
  and the label only), but the API takes it and the generated CLI exposes it, so both defects were
  reachable by the paths the issues named.
- **Consequence:** `Ordinal` joins the system label data map, widening
  [ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)'s closed map by one
  key, and the shipped global system rule deliberately does not use it: for a suppressed first name
  the number and the name disagree, so whether a label says "Boardroom" or "Boardroom 1" is an
  authoring choice an operator makes, not a platform default.

  **Amended ([#693](https://github.com/hyperscaleav/omniglass/issues/693)): the shipped system rule
  reads the ordinal, and the key it reads is the number the NAME carries.** The decline above is
  reversed, and the case that reverses it is the one AV estates are full of: a divisible boardroom is
  two `board` systems in one room, so both rendered "Boardroom", and the operator reading the console
  had less information than the platform holding `boardroom` and `boardroom-2`. The rule is now
  `{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}`, the component's verbatim.

  The reversal is TWO changes and only one of them was named when the ruling was made, which is worth
  recording because the other one is where the argument lives. Adding `{{if .Ordinal}}` to the rule
  alone renders **"Boardroom 1"** for the only boardroom in a room, because `{{if}}` on a string is
  false for the EMPTY string and a suppressed first name still owns the stored ordinal 1. That is the
  defect this entry's decline was protecting against, and a rule change on its own walks straight
  into it. So the map's value changes with it: `Ordinal` is now the ordinal the row's name SHOWS,
  which is empty for a suppressed first exactly as it is empty for a row an operator named. The two
  states read alike to a rule because they mean the same thing to a reader: this row's name carries
  no number.

  The suppression is asked of the MINT (`nameMint.suppresses`, the branch `name` itself takes) rather
  than read off the name string, which keeps this entry's central decision true on the label side: a
  name's shape has one implementation, and a second reading of it could disagree at exactly ordinal
  1. It also survives the seam above, where `bareFirst` becomes per-TYPE: a rule hardcoding
  `ne .Ordinal "1"` would have been a per-kind default baked into a template, wrong for the first
  type that chose differently, and unwritable by an operator who should never have to know which
  mint named a row.

  What the reversal costs is stated rather than buried: a rule can no longer render "Boardroom 1" for
  a system named `boardroom`, because the number is not reachable from the map any more. That was the
  authoring choice the decline preserved, and it is the one this entry already calls the defect the
  epic was filed about, so it is refused as a rule rather than offered and warned about. An operator
  who wants those exact words still types them, which takes the pen (#682) and is the honest way to
  say a label is not derived from anything.

  A key was NOT added beside `Ordinal` to keep both readings available. Two spellings of one number
  in a closed map is a difference for a rule author to misinterpret, and the wrong pick reintroduces
  the defect silently, which is the same argument `NameRule.normalized` makes about two spellings of
  one rule.

  The component tier reads its ordinal through the same helper, and the value there is unchanged for
  every row: `componentMint` does not suppress (a rack's `display-1` beside `display-2` is what an
  operator writes on the label), so the helper's answer is the stored number. One meaning of the key
  on both tiers, rather than two tiers that happen to agree today.

  **The write paths were re-derived and grew by none.** Every act that moves a system's ordinal is an
  act that re-runs the mint or hands the pen back: create, `:rename` (which clears it), `:resetName`,
  `:move` where the bucket changes, and the `system_type` half of a reclassify, which is the same set
  this entry derived for the NAME. Each already restamps the label unconditionally in its own
  transaction, and unconditionally is what matters rather than the set being the same: a reclassify
  between two stems can leave the name identical while the ordinal moves (`wall-2` at ordinal 2 is
  the same string as the suppressed first of stem `wall-2`), so a stamp gated on the name having
  changed would have missed it. A delete frees a lower ordinal and re-mints nothing, by this entry's
  own allocation rule, so no surviving row's label goes stale behind it. The completeness invariant
  (`TestNoActLeavesALabelStaleAnywhere`) now runs a system rule that READS the ordinal, where the one
  it ran before could not have seen a hole in any of this.
- **Tracked under** [#686](https://github.com/hyperscaleav/omniglass/issues/686), the sixth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), and amended by
  [#693](https://github.com/hyperscaleav/omniglass/issues/693).
