---
title: "A floor is named by the operator, not by the allocator"
date: 2026-08-11
refs: []
form: bullet
---

- **A floor is named by the operator, not by the allocator** (folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) as an architect-directed reversal,
  [ADR-0103](/architecture/decisions/#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label)
  amended, [ADR-0102](/architecture/decisions/#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)
  amended). ADR-0103 met the estate's floors and kept the divergence: the West Building's floor is
  the building's Level 2 and a positional generator calls it `1`, and the two fields were held to be
  answering two different questions. The architect rejected that for floors, and the argument that
  settles it is one the entry did not have. A floor's designation **is not an integer**. Buildings
  sign B2, LG, G, M, 1, 12A, P3, so an allocated ordinal is the wrong KIND of value rather than an
  imprecise one, which is what makes this obviously right instead of a matter of taste. It also
  dissolves the objection that looks hardest, since a negative floor is unspellable under the name
  rule but nobody signs a floor `-1`, they sign it `B1`, which is already a legal name.

  So `floor` loses its `name_rule` and joins campus, building and room as nominal, and the dev
  estate's two floors are named `level-2` and `level-1` for the designations they actually carry.
  The shipped location rule renders "Level 2" and "Level 1" from those names, so the two pins
  ADR-0103 called load-bearing are released and the estate's remaining hand-typed labels drop from
  nine to seven: name and label are one fact now rather than two that disagreed.

  **The cost is stated rather than hidden, which is most of the work.** No seeded location type
  carries a rule any more, so location name generation ships **dormant**: correct, tested, and
  demonstrated by nothing in a shipped estate. Seeding a fifth positional type to keep the demo
  alive was refused for the reason seeding a Level 1 was. Nine cases reached the generator through
  the SEEDED floor, and they now reach it through a positional type the tests create (a parking
  deck, whose number genuinely is an arbitrary disambiguator), because a feature losing its coverage
  when its last shipped user goes away is how one quietly stops working.

  The second cost is the one the seed model imposes and no amount of care avoids. `SeedLocationType`
  is insert-when-absent, so **removing the line un-ships nothing from an estate that already booted
  with it**: its `floor` still carries `{"stem": ""}` and still names floors `1`. That is ADR-0102's
  own consequence read backwards, and it composes badly with the other limit recorded there, since a
  rule cannot be CLEARED on the wire either (an omitted key and an explicit `null` both decode to a
  nil pointer). Such an estate reaches the new default only by a direct write, and a test now pins
  both halves rather than leaving them to be discovered: the re-seed leaves the rule standing, and a
  patch that omits `name_rule` leaves it standing too.
