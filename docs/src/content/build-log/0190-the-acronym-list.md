---
title: "The acronym list"
date: 2026-08-11
refs: []
form: bullet
---

- **The acronym list**
  ([#684](https://github.com/hyperscaleav/omniglass/issues/684), the fourth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657)). The word "dictionary" is what this
  slice is named after and the smaller half of what it did. `title` upper-cases a word's first letter,
  so `dsp` rendered `Dsp`, and fixing that is a list. Making the list something an operator can edit
  is what turned a package-level constant into a value with a lifetime, and that is the part slices 5
  and 7 consume.

  The settings engine grew **list support** first, which was four separate places rather than one: a
  `default` tag that has to become a typed slice, a write validator that has to admit a JSON array, a
  merge that replaces rather than merges, and a typed decode that has to turn `[]any` back into
  `[]string`. Only the first needed code. The comma-separated tag form is Huma's, followed exactly so
  one tag cannot produce two disagreeing defaults, and the JSON-array spelling Huma also accepts is
  refused loudly here rather than parsed a second way. `label` is the first `platform` namespace and
  the first `platform,client` one: admin-write because a label is stored once and read by everybody,
  client-readable because the console renders one from the same list the server did.

  The engine's lifecycle is the decision worth reading
  ([ADR-0099](/architecture/decisions/#adr-0099-the-acronym-list-is-one-replaceable-setting-not-a-shipped-list-plus-operator-additions)).
  Parsing binds a template's FuncMap, so a compiled rule carries the dictionary it was parsed
  against, and a mutable engine would leave rules rendering from a dictionary nobody can see. A
  change therefore builds a **replacement**, cached against the dictionary itself rather than a
  generation counter, because a counter has a failure mode a content key cannot have: the second
  write path that forgets to bump it. The gateway resolves the setting when it renders, so an
  operator's edit reaches the next write with no restart, and the render functions take an engine so
  the bulk recompute can resolve one for fifteen thousand rows instead of fifteen thousand.

  One defect found by reading rather than by a failing test, and worth the slice's most careful test.
  The dictionary read was issued against the pool from inside the transaction that was stamping the
  row, so it took a SECOND connection while holding one. A pool whose connections are all held by
  writers each waiting for a second connection is deadlocked, not slow, and a bulk import of the
  estate sizes this epic exists for is enough to reach it. The resolver now takes an override level
  the caller read on its own transaction, and a pool of exactly one connection is the whole
  population of that race, so the regression test reaches the deadlock deterministically rather than
  hoping for it under load.

  Splitting parse from render is what kept the ripple small. `label.New` binds the same four function
  names whatever dictionary it holds and the grammar check walks a static allowlist, so whether a
  rule parses is a fact about the rule alone: the ten rule-validation call sites keep a
  dictionary-less engine and only the three render paths carry the current one.

  The console needed the work nobody scoped: it renders every setting through `String(value)` and
  patches back what it read, which for a list is the joined string and a 422 the server is right to
  give. A list now reads as one comma-separated line and writes as an array, decided from the
  generated `"type": "array"` rather than from the value's shape, so a list that resolved empty is
  still edited as a list.

  The honest limit is in the docs rather than left to be discovered: vendor model numbers are
  unbounded, so this list degrades quietly and forever. That is acceptable for a fallback one click
  from being overridden on the row, and it is why the dictionary matters less than it first appeared:
  a type's `display_name` already carries correct casing, and the ladder reads that before it ever
  re-cases a raw name.
