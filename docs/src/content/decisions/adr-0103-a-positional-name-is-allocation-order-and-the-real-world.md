---
title: "A positional name is allocation order, and the real-world designation is a label"
date: 2026-08-11
status: "Accepted"
summary: "A positional name is **allocation order**, never a claim about the world, and the entry first kept the dev estate's divergence (a floor named `1` labelled Level 2) on the argument that a name is an address and a label is what a human reads. **Amended (#657) and REVERSED for `floor`:** a floor's designation is not an integer at all (B2, LG, G, M, 12A), so an ordinal is the wrong KIND of value for it rather than an imprecise one, and the basement objection dissolves with it, since nobody signs a floor `-1`, they sign it `B1`, already a legal name. `floor` becomes nominal, the dev estate's floors are named `level-2` and `level-1` for their real designations so ADR-0105's rule renders those labels and the two pins are released, and the cost is stated rather than hidden: NO shipped location type carries a rule, so location name generation ships **dormant**, kept covered by a positional type the tests create rather than by a fifth seeded type invented to keep the demo alive. Removing a shipped rule reaches new estates only (insert-when-absent), and no `PATCH` can clear one, so an estate that already seeded it keeps it. What survives: a stem-less positional name is right where the position is an arbitrary disambiguator (a parking deck, a rack row) and wrong where the number is a real-world fact"
---

- **Date:** 2026-08-11 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [location types](/guides/admin/location-types/)
- **Context:** [ADR-0102](#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)
  shipped `floor` as the one auto-nameable location type, positional and stem-less, on the argument
  that "a floor has a name the platform can know". The dev estate is the first real data to meet
  that argument, and it falsifies the sentence as written: the West Building's only seeded floor is
  the building's **Level 2**, and the generator calls it `1`, because a positional ordinal is the
  lowest free number in the parent's bucket and nothing else.
- **Decision:** `floor` stays positional, and the divergence is **kept rather than removed**, because
  the two fields are answering two different questions. A **name** is an address: it has to be
  unique under its parent, legal, and typeable, and the platform owning it means an operator did not
  have to think of one. A **label** is what a human reads, and a building's own designation for a
  floor ("Level 2", "B1", "Mezzanine", the skipped 13) is signage, a real-world fact the platform has
  no access to. So the platform allocates the name and the operator types the label, and the seeded
  estate ships both cases of the same type side by side: the floor under Innovation Hall is named `1`
  and labelled Level 1 (they coincide), and the floor under the West Building is named `1` and
  labelled Level 2 (they do not).
- **Decision (the sharper rule this generalises to):** a stem-less positional name is right where the
  position is an **arbitrary disambiguator** and wrong where the number is a real-world fact. It is
  never a claim about the world, so a type whose ordinal an operator will read as a designation
  (a rack unit, an output, a channel) wants its number **typed**, not allocated. `floor` sits on that
  line rather than safely inside it, and it stays shipped-positional on the strength of the workflow
  it serves: building out a tower floor by floor, where typing forty names is the cost and the
  designations arrive later as labels.
- **Rejected, and why each:**
  - **Make `floor` nominal and drop its rule** (the plain reversal of ADR-0102). It is the more
    honest reading of "a floor has a name the platform can know", and it was rejected on cost rather
    than principle: it leaves the shipped estate with **no** auto-nameable location type, so the
    feature is inert everywhere until an operator defines one, and it moves nine storage cases and an
    e2e that assert against the seeded `floor` rule. The reversal stays available and this entry is
    where it starts. **This is the option the amendment below takes.**
  - **Seed a Level 1 under the West Building so the numbers line up.** It makes the estate agree by
    construction and teaches the wrong lesson: correspondence would look like a guarantee, and it is
    a coincidence of enumerating from the bottom in order.
  - **Ship a `floor` label rule (`Floor {{.Name}}`).** It renders "Floor 1" for a floor that is
    Level 2, which is the same defect with a nicer typeface.
- **Consequence:** the shipped **location** label rule stays empty and the dev estate labels all
  thirteen of its locations by hand, which is not a gap in the demonstration but the demonstration:
  a location's label is operator space by design, and the two floors are where that stops being an
  abstraction. It also means a positional name is not safe to read as a designation anywhere in the
  product, so a console surface that shows a floor must show its label. (The empty half of this is
  superseded by [ADR-0105](#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused),
  which ships a global location rule; the operator-space half stands.)
- **Amended (#657), and REVERSED for `floor`:** the architect rejected the divergence on this type,
  taking the option listed as rejected-on-cost above. Two arguments, and the second is the one this
  entry did not have.

  **The allocator's answer misreads as a designation.** A building with floors 0, 1 and 2 is named
  `1`, `2` and `3`, in whatever order somebody seeds them. This entry called that friction worth
  keeping; the architect calls it an invitation to misread the estate, and the invitation is extended
  by the platform rather than by the operator.

  **A floor's designation is not an integer at all.** Real buildings sign B2, LG, G, M, 1, 12A, P3.
  Modelling that as an ordinal is a **category error**, not merely an imprecision, which is what makes
  the reversal obviously right rather than a matter of taste. It also dissolves the objection that
  looks hardest: a negative floor cannot be spelled under `^[a-z0-9][a-z0-9-]*$`, since a leading
  hyphen is refused, but nobody signs a floor `-1`, they sign it `B1`, which is already a legal name.
  A rule shaped like `.Ordinal` could never have reached that name whatever the sign handling.

  So `floor` becomes **nominal**: no `name_rule`, and an operator names it exactly as they name a
  campus, a building and a room. The dev estate's two floors are named `level-2` and `level-1` for the
  designations they actually carry, ADR-0105's shipped location rule renders "Level 2" and "Level 1"
  from those names, and the two pins this entry made load-bearing are **released**: name and label are
  one fact now instead of two that disagreed.
- **Consequence of the reversal (the cost, stated rather than hidden):** of the four seeded location
  types **none** carries a name rule, so **location name generation ships dormant**: correct, tested,
  and demonstrated by nothing in a shipped estate. Seeding a fifth positional type to keep the
  demonstration alive is refused for the reason seeding a Level 1 was, that a demonstration
  constructed to make the feature look used teaches the wrong thing. The generator keeps its coverage
  through a positional type the TESTS create (a parking deck: its number is an arbitrary
  disambiguator, which is exactly what a floor's is not), because a feature losing its tests when its
  last shipped user goes away is how one quietly stops working.
- **Consequence of the reversal (an existing estate keeps the rule, and cannot drop it on the wire):**
  `SeedLocationType` is insert-when-absent, the forked-template half of the seed model, so deleting
  the line un-ships nothing from an estate that already booted with it: its `floor` still carries
  `{"stem": ""}` and still names floors `1`. That is the mirror image of ADR-0102's own consequence
  (the rule reached new estates only), and it is not fixable by a backfill, because a `location_type`
  row is operator space. It is sharper than the shipping direction was, because ADR-0102's second
  recorded limit bites here: a rule **cannot be cleared on the wire**, only replaced, since an omitted
  key and an explicit `null` both decode to a nil pointer. Such an estate reaches the new default only
  by a direct write, and the operator guide says so rather than implying a release does it. Closing
  that needs the wire-contract change ADR-0102 costed and declined.
- **What survives for genuinely positional types:** everything except the placement of `floor`. A
  stem-less positional name is still right where the position is an **arbitrary disambiguator** (a
  parking deck, a rack row, a berth) and wrong where the number is a real-world fact an operator reads
  off a wall; `floor` sat on that line and is now placed firmly on the nominal side of it. The
  name-versus-label distinction stands untouched: a name is an address, a label is what a human reads,
  and the two coinciding on the dev estate's floors is a property of naming them well, not a
  guarantee. A console surface that shows a location still shows its label.
- **Tracked under** [#689](https://github.com/hyperscaleav/omniglass/issues/689), the ninth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657); the amendment folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review.
