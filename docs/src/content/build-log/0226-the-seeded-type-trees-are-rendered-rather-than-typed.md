---
title: "The seeded type trees are rendered rather than typed"
date: 2026-08-13
refs: []
form: bullet
---

- **The seeded type trees are rendered rather than typed**
  ([#678](https://github.com/hyperscaleav/omniglass/issues/678)). `seed.json` carried eleven shipped
  sets and not the two inheriting classification registries, so the only copy of the `system_type` tree
  in the live docs was a sentence in [core entities](/architecture/core-entities/) naming all eleven
  rows and their nesting, and the `component_type` section taught its taxonomy through examples that
  stopped at "...". The issue's own framing was half right and worth correcting: there was no CLOSED
  hand-written enumeration of `component_type` in the live docs, only an open-ended one, which is a
  different defect (a set that cannot drift because it never claimed to be complete, and also never
  told an operator what ships).

  Both registries now render from the seed, and the render keeps a blank fact BLANK. That is the whole
  point of publishing them: `board` states its own stem and abbrev and leaves its icon to `room`, and a
  table that resolved the chain on the way out would teach the opposite of
  [ADR-0095](/architecture/decisions/)'s first-non-null walk while looking tidier. The location types
  the same bullet list enumerated a second time now point at the guide that already renders them, so
  the estate's shipped shape is stated in exactly one place per set.

  The drift test grew the assertions the shape needs rather than a row count: a child points at its
  parent, a root carries the stem it has no ancestor to inherit, an inheriting row's fact stays empty,
  and no row names a parent declared after it, which would silently drop a subtree out of the rendered
  tree.
