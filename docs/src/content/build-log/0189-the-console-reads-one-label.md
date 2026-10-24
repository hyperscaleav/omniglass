---
title: "The console reads one label"
date: 2026-08-11
refs: []
form: bullet
---

- **The console reads one label**
  ([#683](https://github.com/hyperscaleav/omniglass/issues/683), the third slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657)). Slice 2 stored the pen and never put
  it on the wire, so the slice starts with an API change rather than a console sweep:
  `display_name_generated` joins the component, system and location read bodies, read-only for the
  same reason `name_generated` is, and the operator claims or surrenders it by writing the label
  rather than by asserting a boolean.

  `hasDisplayName` decided whether a row shows its name on a second line, and it decided it by
  comparing the label to the name. That was the same question as "did a human choose this" only while
  a label was only ever operator-typed. A rule-rendered label differs from the name exactly as an
  operator's does, so unchanged it would have put a second identifier line under every row in a
  15,000-component estate. It now reads the pen, with the string comparison kept as the second half of
  a conjunction, because a rule with nothing to say about a row keeps the pen and stores no label
  ([ADR-0098](/architecture/decisions/#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)):
  holding the pen does not imply there is a label to attribute. `labelGenerated` is the other half and
  marks the row, and `labelIsName` is the face question `BladeTitle` had been asking by hand.

  Forty call sites were hand-rolling the renderer beside it, in three dialects, and the classification
  mattered more than the count. Twenty-nine were labels. Two were **sort keys**, where converting
  changes ordering, and three were `<option>` and picker rows where the **value** is submitted and the
  text beside it is not: every value was left exactly as it was, which is the [#644](https://github.com/hyperscaleav/omniglass/issues/644)
  defect class this slice was warned about. Fifteen were **filter facets**, and those were the ones
  already broken: a facet spelling its haystack `` `${r.name} ${r.display_name}` `` searched the
  literal text "undefined" on every unlabelled row, so typing "undefined" matched them all and typing
  a label matched none. Two role pickers fell back to a **uuid** rather than the role's name, and two
  registry lookups rendered the empty string for a found row with a blank label, because `??` does not
  treat `""` as absent.

  The sweep is held by a source guard rather than by page tests, for the reason the vocabulary guard
  exists: the page nobody wrote a test for is the page that drifts, and this rule had drifted in forty
  places. It is line-precise, not file-precise, since a file-level exemption is how half a sweep hides
  behind a neighbour that had a good reason (`lib/principals.ts` is exactly that file: its role facet
  goes through the primitive and `principalName` beneath it does not). Four exceptions survive, each a
  rule that is genuinely not the entity label's, and each asserted to still match so a stale entry
  fails too.

  The command palette does not follow, and that is the finding rather than an omission: it is a
  navigation jumper over static rail and catalog labels and holds no entity at all, so there is
  nothing in it to render through the primitive. Finding an entity by its label is a surface that does
  not exist yet.
