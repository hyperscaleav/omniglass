---
title: "A rule can read a name as words, so the dictionary reaches something"
date: 2026-08-11
refs: []
form: bullet
---

- **A rule can read a name as words, so the dictionary reaches something** (folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review,
  [ADR-0105](/architecture/decisions/#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused)).
  Ten slices built a label engine and an operator-owned acronym dictionary that no rule could reach
  from a NAME. `title` upper-cases each word and leaves the separator standing, so `{{title .Name}}`
  rendered `north-wing` as "North-Wing", and the closed FuncMap's other three ran the other way or
  ignored word boundaries entirely: "HQ West" out of `hq-west` was unwritable in any spelling, which
  is most of what the dictionary was for. `words` is the missing half, `slug`'s opposite number: a run
  of `-` or `_` becomes one space, a leading or trailing run is dropped rather than becoming an edge
  space, and everything else, including whitespace the fact already carried, is untouched. It is
  deliberately not `wordRe`'s "anything that is not a letter or a digit", since a catalog display
  name's parentheses and slashes are punctuation somebody chose.

  Adding a function to a closed grammar is a **three-place act** (the FuncMap, the AST allowlist,
  `FuncNames`), and two of the three is silent in both directions: a name in the FuncMap alone parses
  nowhere, and a name in the allowlist alone is just a hole in the allowlist. The published set is
  therefore walked by a test that parses each name in a real rule, so the three places are enforced
  rather than described.

  The **global location rule** then ships as `{{title (words .Name)}}`, which reverses half of the
  argument the seed file made against it. It said any rule at that tier is "either a constant or a
  restatement"; the constant half stands (labelling every room "Room" is worse than the name it would
  replace) and the restatement half was true only while a restatement could echo. This one re-cases
  the name and runs the operator's dictionary over it, so it produces a string the read ladder's last
  rung cannot. That rung is unchanged and still verbatim: a row with no stored label renders its name
  exactly. Shipping the rule restamps nothing by itself, because the blast radius is the whole estate
  and waits for the verb, so an existing install keeps its raw kebab locations until an operator runs
  `/locations:recomputeLabels`, and the operator guide says that rather than implying a release does
  it for them.

  The dev estate stopped masking it. #689 pinned a `display_name` on all thirteen locations because
  nothing would have rendered one; four of those pins now restate exactly what the rule renders, so
  they are released and the RENDERED values pinned in their place, which is what keeps an assertion
  behind a released pin. Nine survive with a reason each: `hq`, `west`, `east` and `airport` because a
  bearing or an abbreviation is not a place, `huddle`, `briefing` and `hall` because the room's noun
  is not in its name, and ADR-0103's two floors because a positional name is allocation order and
  releasing those two deletes that worked example from the estate. (Seven, after the floor reversal
  at the end of this log took the last two: a floor named for its designation renders that
  designation.) The media lab's name became
  `media-lab`, since every other location name was one word and nothing in `make dev` would otherwise
  show a separator becoming a space.
