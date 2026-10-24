---
title: "The dev estate stops naming itself"
date: 2026-08-11
refs: [689]
form: bullet
---

- **The dev estate stops naming itself** ([#689](https://github.com/hyperscaleav/omniglass/issues/689),
  the ninth slice of [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0103](/architecture/decisions/#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label)).
  Eight slices built a name generator that the one estate anybody actually looks at did not use: every
  fixture row in `internal/devseed` hand-wrote its own name, and seventeen of the twenty-two hand-wrote
  their ancestry into it as well (`hq-west-2-boardroom`, `boardroom-a-panel`), which is the habit
  placement-scoped names retired two slices before this epic began. Now nothing in the estate names
  itself except the rows no rule could name. Its devices, its systems and its floors ask the platform,
  so `make dev` comes up on the generator's own output: three rooms each holding a `display-1`, a
  divisible boardroom whose halves are `boardroom` and `boardroom-2`, and two buildings each with a
  floor called `1`.

  The mechanical crux was that a generated name cannot be the fixture's identity, because the platform
  picks it and the fixture still has to say "put this component in that room" and still has to
  recognise its own rows on the second `make dev` of the morning. So the fixture grows a `key`, local
  to the document, never written to the database, and every reference resolves through it to a row
  **id** rather than a name (an id is a legal reference wherever a name is, ADR-0062, and it has to be
  one here: a bare `display-1` now matches three rows on purpose). Recognising a row on a later run is
  the other half, and it is answered by the fixture's real claim: it does not say "there is a display
  called display-2 in the boardroom", it says "the boardroom holds three displays". A row the fixture
  names is found by name as before; a row the PLATFORM named is found by its position among the
  platform-named rows of the same classification in the same bucket, in the order the generator
  allocated them.

  The ruling the slice owed was the nominal-versus-positional question meeting real data for the first
  time. The West Building's only floor is the building's Level 2, and a positional generator calls it
  `1`, because a positional ordinal is the lowest free number in the bucket and nothing else. The
  divergence is KEPT: a name is an address and a label is what a human reads, and a floor's
  designation is signage the platform has no access to. (Reversed later on the same branch, see
  the floor entry at the end of this log: a designation is not an integer at all.) The estate now ships both cases of one type
  side by side, a floor named `1` labelled Level 1 and a floor named `1` labelled Level 2, which is
  the clearest thing in it. Seeding a Level 1 so the numbers would line up was refused as
  concealment; making `floor` nominal, the plain reversal of ADR-0102, was refused on cost rather than
  principle and is written down as still available.

  The labels moved the same way. A set `display_name` takes the pen, so an estate that set one on
  every row demonstrated the exact opposite of the label rules: components now render theirs (`Display
  1`, `Video Bar 2`) from the shipped rule over the resolved type and the allocated ordinal, and the
  survivors are enumerated with a reason each. The unclassified power conditioner keeps its typed
  label because the rule can only render "Generic Device 1" for a box with no product; the two
  boardroom halves keep theirs because which half is A is a fact about the air wall, and because the
  shipped system rule reads the type and would label both of them alike; every location keeps its own
  because no location rule shipped yet (four of those thirteen pins were released when one did, and
  two more when the floors were named for their designations, see the words and floor entries at the
  end of this log). Two pure fixture tests hold that line by
  key, so a fourteenth typed label is a deliberate edit rather than a quiet one, and the integration
  suite asserts every seeded name and label by value.
