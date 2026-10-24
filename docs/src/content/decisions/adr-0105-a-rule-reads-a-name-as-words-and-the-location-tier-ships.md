---
title: "A rule reads a name as words, and the location tier ships the restatement it once refused"
date: 2026-08-11
status: "Accepted"
summary: "`words` joins the closed FuncMap (a run of `-` or `_` becomes one space, an edge run is dropped, everything else untouched), which is what finally lets a rule turn a kebab NAME into words: `title` alone leaves the separator standing, so the acronym dictionary of ADR-0099 could not be reached from a name by any spelling. Adding a function is a THREE-place act (FuncMap, AST allowlist, `FuncNames`) and the published set is now walked by a test rather than described. **Amended (#701):** it is ONE place; all three derive from a single declaration and the docs table is a fourth reader of it, so a function in the FuncMap the grammar refuses is unwritable rather than tested for. The global LOCATION rule ships as `{{title (words .Name)}}`, reversing the seed's own argument on its restatement half only: a restatement that RE-CASES and runs the operator's dictionary produces a string the read ladder's fallback cannot, where an echo could not, and the constant half (\"Room\" for every room) is still refused. The ladder's last rung stays verbatim, since this renders and STORES a label rather than prettifying on read; the estate keeps only the pins that say something a name cannot, nine at the time and **seven** after ADR-0103's reversal named the two floors for their designations"
---

- **Date:** 2026-08-11 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [find things in your estate](/guides/operator/fleet/#finding-one-thing), [location types](/guides/admin/location-types/)
- **Context:** epic [#657](https://github.com/hyperscaleav/omniglass/issues/657) shipped a label engine
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)) and an operator-owned
  acronym dictionary ([ADR-0099](#adr-0099-the-acronym-list-is-one-replaceable-setting-not-a-shipped-list-plus-operator-additions))
  that nothing could reach. `title` upper-cases each word and leaves the SEPARATOR standing, so
  `{{title .Name}}` renders `north-wing` as "North-Wing"; the closed FuncMap's other three run the
  other way (`slug`) or ignore word boundaries entirely (`upper`, `lower`). There was therefore no rule
  an operator could write that turned a kebab NAME into the words in it, and "HQ West" from `hq-west`
  was unreachable by any spelling. It bit hardest on locations, whose global rule shipped deliberately
  empty, so every location in a shipped estate fell to the read ladder's last rung and read its raw
  name.
- **Decision (the function):** `words` joins the closed FuncMap: a run of `-` or `_` becomes one space,
  a leading or trailing run is dropped rather than becoming an edge space, and every other character,
  including whitespace the fact already carried, is untouched. It is deliberately not a normalizer and
  deliberately not `wordRe`'s "anything that is not a letter or a digit": a catalog display name's
  parentheses and slashes are punctuation somebody chose, and only the two separators a NAME is built
  from are spent. Adding a function is a **three-place act** by design (the FuncMap, the AST allowlist,
  and `FuncNames`), and two of the three is silent in both directions, so the published set is now
  WALKED by a test that parses each name in a real rule rather than described by a comment.
  **Amended ([#701](https://github.com/hyperscaleav/omniglass/issues/701)):** the three-place act is
  ONE place, and the "by design" was wrong about which design. What the property needs is that a
  function be in the FuncMap AND the grammar, not that a person type it into both: the FuncMap,
  `FuncNames` and the allowlist now all derive from a single ordered declaration, so two-of-three is
  unwritable rather than merely tested for, and the fourth copy this entry did not count (the prose
  that teaches the language) becomes a table rendered from that same declaration. Adding a function
  stays a deliberate, test-visible act, because the closed-set test still names the members and the
  generated artifact still has to be regenerated and committed. The walking test stands and is joined
  by its converse, since a name admitted by the grammar with no implementation behind it was the
  direction nothing checked.
- **Decision (the location rule):** the global location rule ships as `{{title (words .Name)}}`, written
  to `default_template`, the boot-seed-owned column an operator's `template` sits beside untouched. The
  seed's own comment argued against exactly this and is rewritten rather than deleted: it said any rule
  at that tier is "either a constant or a restatement", and the constant half stands (labelling every
  room "Room" is worse than the name it would replace) while the restatement half was true only while a
  restatement could **echo**. This one re-cases the name and runs the operator's dictionary over it, so
  it produces a string the fallback cannot, which is the test a rule at this tier has to pass.
- **What this does NOT change:** the read ladder's last rung is still **verbatim**. A row with no stored
  label renders its `name` exactly, never a prettified version, and the difference is that this rule
  RENDERS AND STORES a label rather than teaching a read path to guess one. That distinction is
  [#613](https://github.com/hyperscaleav/omniglass/issues/613)'s, corrected the same day for having the
  same confusion in it.
- **Consequence (the upgrade is not automatic, and says so):** a shipped rule change restamps nothing by
  itself ([ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate),
  [ADR-0102](#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)):
  the blast radius is the whole estate, so it waits for the verb. A new estate gets a rendered label at
  create; an existing one keeps its raw kebab locations until an operator runs
  `/locations:recomputeLabels`, and the operator docs say that plainly rather than implying a release
  does it for them.
- **Consequence (the estate stops masking the rule):** [#689](https://github.com/hyperscaleav/omniglass/issues/689)
  pinned a `display_name` on all thirteen dev-estate locations, because nothing would have rendered one.
  Four of those pins now restate what the rule renders, so they are released and the RENDERED values
  pinned in their place; nine survive with a reason each, ADR-0103's two floors among them, since a
  positional name is allocation order and releasing those deletes that worked example. **Amended
  (#657):** ADR-0103 was then reversed for `floor`, so those two floors are named `level-2` and
  `level-1` for their designations, this rule renders "Level 2" and "Level 1" from those names, and
  their pins are released too. Six rendered values are pinned in place of released ones and **seven**
  survive. The media lab's
  name becomes `media-lab`: every other location name in the estate is one word, so without it nothing
  the console shows demonstrates a separator becoming a space.
- **Known gap, deliberately not closed here:** `HQ` is not in the shipped acronym list, so `hq-west`
  renders "Hq West" out of the box and needs one operator edit to read "HQ West". Adding it is a change
  to a settings DEFAULT rather than to this rule, it ripples through three generated artifacts, and
  ADR-0099's replace-not-merge semantics mean a shipped entry is not free. Left as a follow-up so this
  slice stays what it says it is.
- **Tracked under** epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review.
