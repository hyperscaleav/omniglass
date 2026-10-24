---
title: "A name rule is a declaration a type opts in with, and a rule change renames nothing"
date: 2026-08-10
status: "Accepted"
summary: "A **`location_type.name_rule`** (nullable jsonb) is a type's opt-in to naming the locations it classifies, and it is a **declaration** (a stem, possibly empty, plus the first-ordinal suppression flag) rather than the `label_rule` template beside it. A name has to satisfy `validateEntityName`, it lands in a scoped-unique index, and other things reference it, so an unrenderable rule has no safe degradation the way a label's does; a declaration IS a `nameMint`, so a rule is refused at RULE-EDIT time by minting from it (ordinal 1 and a nine-digit ceiling bound the whole output space), which a template's output could only be sampled. Null is the opt-out and there is no boolean beside it. A rule change **renames nothing**: there is no name-side recompute verb, deliberately, because relabelling in bulk is recoverable and renaming is not. A positional type permitted at root allocates `1`, `2` across the estate and that is legal, since the bucket is the placement and two positional types under one parent already share an ordinal space. **Amended (#657):** the entry's \"only floor is genuinely auto-nameable\" is now false in both halves, since ADR-0103 was reversed for `floor` and no shipped type carries a rule; the composed limit is that a withdrawn shipped rule cannot be un-shipped, because insert-when-absent leaves the row alone and the wire cannot spell \"clear\" | **Amended again (#710):** the console gets the editor a declaration makes small (a tick box, a stem, the suppression flag), with #692's clear moved INSIDE it so the affordance can no longer undo a state it cannot create, and the mask naming every field the blade writes since a mask governs the whole write. What the editor shows about what a rule PRODUCES comes from the server: `name_rule` reads back with `examples`, the first two names the same `nameMint` a create allocates from would stamp, so nothing in `web/` knows that a counted name is `<stem>-<n>`. An unsaved rule is shown nothing about its output, deliberately. The mint's own refusal is unreachable through this body (`stem` carries `maxLength: 90`, and 90 plus the widest provable ordinal is exactly the name cap), so the 422s a surface renders are the schema's, which is why `describeError` now appends Huma's `errors[]`"
---

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision (the opt-in):** `location_type` gains a **nullable `name_rule`**, and its presence IS
  the opt-in: null means an operator names every location of that type. There is no boolean beside it,
  so "this type generates" and "this is what it generates" cannot disagree. A campus, a building and a
  room each have a real-world name an operator holds and the platform does not, so generating one
  would be guessing. `17c` is ground truth, not a default.

  This entry shipped saying "of the shipped place vocabulary only **floor** is genuinely
  auto-nameable, and it ships positional", so three of the four shipped types stayed on the opt-out.
  **Amended (#657): that is now false in both halves.**
  [ADR-0103](#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label)
  was reversed for `floor` and the rule was dropped from the seed, on the argument that a floor's
  designation (B2, LG, G, M, 12A) is not an integer at all, so **all four** shipped types are on the
  opt-out and no shipped type is auto-nameable. The opt-in itself is unchanged, and it is reached by
  an operator declaring a positional type of their own. What that costs, and why no fifth type was
  seeded to replace it, is recorded on ADR-0103 rather than repeated here.
- **Decision (a declaration, not a template):** The rule is `{"stem": "...", "bare_first": <bool>}`,
  decoded straight into the `nameMint`
  [ADR-0101](#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests)
  already made the one place a generated name's shape lives. An empty stem is a **positional** type,
  whose ordinal genuinely is its name (a floor called `1`).

  The alternative, and it was the more consistent-looking one, was the `label_rule` template beside
  it on the same table: the same `text/template` engine, the same allowlist
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)), reusing a parser
  rather than growing a second dialect. It was rejected on three counts, and the first is decisive.

  **A name has no safe failure.** A label rule that fails to render degrades: the row keeps the pen,
  the read ladder falls through to the entity's name, and nothing is lost. A name has no next rung.
  It is `NOT NULL`, it is in a scoped-unique index, it is the address an operator types, and it is
  what a runbook outside this system stores. A template that renders `Floor-1` for a type whose
  display name is `Floor` produces an illegal name, and the honest options at that point are a failed
  create or a silently different name, neither of which is a degradation.

  **The edit-time refusal is provable for a declaration and only sampleable for a template.**
  [#686](https://github.com/hyperscaleav/omniglass/issues/686)'s acceptance already promised that a
  rule which would render an illegal name is refused when the RULE is edited, not when a location is
  created, and nothing had built it. A declaration's whole output space is `{stem}` and
  `{stem-n : n >= 1}`, so `validateNameRule` mints ordinal 1 and a nine-digit ceiling and checks
  both: every candidate between them shares a character class and has a length between the two, so a
  rule legal at both ends is legal everywhere in it. The ceiling has to exist rather than being
  waved at, because `<stem>-<n>` grows with `n` and the name rule has a length cap: a 97-character
  stem mints legally at 1 and illegally at 100. A template could only have been probed against a
  sample row, which is a different and weaker promise.

  **The expressiveness would have bought nothing here.** A location's data map
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)) carries `Name` and
  `TypeName`. `Name` is what a name rule is producing, so it is circular; `TypeName` is the type's
  display name, whose slug IS the stem. The one useful template a location could write is the
  declaration, spelled longer. The FuncMap would have diverged too (`slug` and `lower` matter,
  `title` and the acronym dictionary are actively wrong for a kebab name), so "reuse the engine"
  would have meant a second configuration of it, not a second use of it.

  Where a template earns its place is a rule reading facts a declaration cannot name, which is the
  shape a future component or system name rule might have (a vendor, a product, a role). That is a
  reversal to make when there is a fact to read, on the tier that has one, with an argument about
  failure that this tier cannot make.
- **Decision (the rule IS the mint, so nothing restates the shape):** `NameRule.mint()` returns a
  `nameMint` with no reshaping, which carries ADR-0101's guarantee onto this tier for free: the
  allocator tests `mint.name(n)`, the caller mints from the same value, and there is no second
  spelling of the shape to keep in step. Validation mints from it too, so the check and the generator
  cannot disagree about what a rule produces. A stem-less rule normalizes `bare_first` to false,
  since suppressing the ordinal of a name that IS its ordinal would leave nothing: two spellings of
  one rule would compare unequal while minting identically.
- **Decision (a rule change renames nothing, and there is no name-side recompute):**
  [#687](https://github.com/hyperscaleav/omniglass/issues/687)'s own acceptance said a rule change
  renames nothing "except through the explicit recompute" of
  [#685](https://github.com/hyperscaleav/omniglass/issues/685). That verb is the **label** cascade,
  and there is no name-side equivalent. Editing a rule changes no stored fact about any existing row:
  the name, the ordinal and the pen all stand, and the new rule decides how the NEXT nameless create,
  `:resetName`, move or reclassify names a row.

  That is ADR-0100's blast-radius line, which ADR-0101 already applied to a `system_type`'s stem, and
  it is stronger on the name side rather than merely the same. A bulk relabel is recoverable: a label
  is display, the pen says who owns it, and the preview shows the blast radius before the apply. A
  bulk rename is not: it breaks bookmarks, runbooks and integration config outside this system,
  which is the entire reason a rename is its own verb under its own permission
  ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)).
  `:resetName` is the deliberate, one-row-at-a-time way to bring a row onto a new rule, and it is
  gated by `location:rename` because it IS a rename.
- **Decision (a positional type at root is legal):** `allowed_parent_types` can permit a positional
  type at root, where the bucket is every parentless location, so it allocates `1`, `2` across the
  whole estate. That is allowed, and the argument is that the root bucket is not a special case, it
  is a large one. The bucket is the PLACEMENT, never the placement and the type, so two positional
  types under one parent already share an ordinal space; refusing at root would refuse the
  estate-sized instance of a rule that holds everywhere else, and bucket size is not a property the
  platform can police (a building with two hundred floors is the same shape). An operator asks for it
  by making one type both positional and root-placeable.

  The real consequence is not allocation, it is ADDRESSING: a positional type makes a bare name
  ambiguous by design, since every building has a floor `1`. That is answered by machinery this epic
  did not have to build, the dotted address
  ([ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)'s addressing and
  `#627`'s placement-scoped uniqueness): `boi.17c.1` resolves where `1` cannot, and an ambiguous bare
  name is already a named refusal listing its candidates rather than a silent first match.
- **Consequence (two limits recorded rather than papered over):** A rule cannot be CLEARED on the
  wire, only replaced, because an omitted JSON key and an explicit `null` both decode to a nil
  pointer and an object field has no third state to read. That is the limit
  `component_type.stem`'s patch already carries (its wire field is `minLength: 1`), and closing it
  means either an untyped `json.RawMessage` field, which costs the generated client its shape, or a
  custom method for one field; both are wire-contract choices rather than this slice's. And the
  shipped `floor` rule reaches NEW estates only: `SeedLocationType` is insert-when-absent, the
  forked-template half of the seed model, so an upgraded estate opts its own `floor` in with one
  `PATCH`. Deliberately no backfill migration, since the rule it would install could not then be
  removed, and a `location_type` row is operator space rather than platform space. Both limits are
  written into the operator guide with the exact call.

  **Amended (#657):** the two limits compose worse than either does alone, which is only visible when
  a shipped rule has to be WITHDRAWN, as ADR-0103's reversal withdraws this one. Removing the line
  reaches new estates only, by the same insert-when-absent argument read backwards, and the estate
  that already has the rule cannot drop it with a `PATCH` either, because the wire has no spelling for
  clearing one. So a `floor` opted in by an earlier release stays opted in until someone writes the
  column directly. The `PATCH` above is still the whole of the opt-IN story; there is no opt-out
  story, and the operator guide now says that rather than leaving it to be discovered.
- **Consequence (a location label rule still cannot read `.Ordinal`, and that is not an oversight):**
  ADR-0098's closed map gains no key here, although this slice gives a location an ordinal. It would
  be redundant where it helps and wrong where it does not: a positional location's name IS its
  ordinal, so `{{.TypeName}} {{.Name}}` already renders `Floor 3`, and the only case a separate key
  serves is a stemmed type with the first ordinal suppressed, where printing `Wing 1` for the only
  wing is the defect the epic was filed about. ADR-0101 declines `.Ordinal` in the shipped SYSTEM
  rule for the same reason.
- **Consequence (the fork primitive does not reach this registry, and does not need to):**
  [#655](https://github.com/hyperscaleav/omniglass/issues/655)'s `registry_shadow`
  ([ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it))
  has exactly one adopter, `component_type`, so an operator cannot fork a rule onto an official
  `location_type` row. They do not have to: the shipped location types are seeded
  `official: false` and inserted only when absent, so they are ordinary operator-owned rows the
  registry's own `PATCH` edits, and a restart never reverts the edit. That is the forked-template
  half of the seed model rather than the canonical-catalog half, and it is the same reason this
  registry has needed no fork for `label_rule` either.
- **Consequence (the write paths, derived again):** A location's mint reads exactly two things, the
  type's rule and the parent bucket, which yields create, `:resetName`, the `location_type` half of a
  reclassify, and `:move`. Two of those are worth stating. The reclassify guard is the classification
  **changing**, not the field being present, because `web/src/pages/Locations.tsx` sends
  `location_type` on every save: the defect review caught on the system tier one slice ago
  (ADR-0101) is refused entry here rather than repeated. And `:move` re-mints only when the parent
  actually moves. That was written as NARROWER than the system tier's, where the same re-mint ran on
  a move that changed no bucket, and the reason given for leaving the system arm wide was that
  narrowing it would move an existing expected value. The epic's review pass established that the
  premise was false: systems gained generated names one slice earlier on the same unmerged branch,
  so every expected value at risk had been written by that branch. **Both `:move` arms now re-mint
  only on a bucket change**, and no test expectation moved. The two are spelled differently because
  the kinds do not have the same number of buckets: a location has two and the parent is the whole
  of the distinction, so comparing the parent IS comparing the bucket, while a system has three with
  a parent winning over a location, so its arm compares the `nameScope` bucket the two placements
  resolve to and a parented system relocated to another room keeps its name.
- **Consequence:** `location` gains the nullable `ordinal` the previous slice deliberately withheld
  from it ("a column no writer can fill is a fact waiting to be read wrongly"), and the
  recompute-and-compare invariant now covers all three trees. The bare render stays unwired here for
  a reason of its own: `location_type` carries no `abbrev` at all, so there is no compact form to
  substitute into, and a positional location's name already is its ordinal.
- **Tracked under** [#687](https://github.com/hyperscaleav/omniglass/issues/687), the seventh slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).
- **Amended (#710):** the console gets the **editor** this entry's "declaration" makes small, a tick
  box, a stem, and the suppression flag, and the rule's PRESENCE stays the opt-in, so the tick box is
  the rule's existence rather than a fourth field. [#692](https://github.com/hyperscaleav/omniglass/issues/692)'s
  clear MOVES INTO it (unticking is the clear) rather than sitting beside it as a button, which is
  what stops the affordance being half a control: it could undo a state it could not create, and
  after ADR-0103's reversal no shipped type carries a rule, so opting IN is the only route to a
  generated location name at all. Because the mask governs the whole write, the clear names every
  field the blade writes rather than `name_rule` alone. **What the editor shows about what a rule
  produces comes from the server**: `name_rule` reads back with `examples`, the first two names the
  rule mints, from the same `nameMint` a create allocates from, and the blade composes its sentence
  around those two strings. Nothing in `web/` knows that a counted name is `<stem>-<n>`, which is
  ADR-0104's refusal of a second implementation applied to a rule rather than to a row. An UNSAVED
  rule is shown nothing about its output, deliberately: the only honest source is the mint, and
  predicting from the browser is the duplication this closes. The character rule for a stem IS
  restated in TypeScript (ADR-0113's inline vocabulary, refusing the save), the 90-character ceiling
  is NOT, because that number is arithmetic over the mint's output space rather than a name rule.
  **The mint's own refusal turns out to be unreachable through this body**: `stem` carries
  `maxLength: 90`, and 90 plus the widest provable ordinal is exactly the 100-character name cap, so
  every stem the schema admits mints legally at both ends and `ErrInvalidNameRule` (still live, still
  unit-tested) is fenced off by the schema. The refusals a surface actually renders are the schema's,
  which is why `describeError` now appends Huma's `errors[]`: a schema 422's `detail` is always the
  literal "validation failed", so every console surface had been showing an operator nothing they
  could act on.
