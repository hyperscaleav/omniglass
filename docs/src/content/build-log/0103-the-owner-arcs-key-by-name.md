---
title: "The owner arcs key by name"
date: 2026-07-21
refs: []
form: bullet
---

- **The owner arcs key by name** ([ADR-0055](/architecture/decisions/#adr-0055-the-tag-variable-and-secret-owner-arcs-key-by-name)):
  the nine arc columns on `tag_binding`, `variable`, and `secret` convert from `uuid references <entity>
  (id)` to `text references <entity> (name) on update cascade`, matching every table from the collection
  era onward and completing what ADR-0053 deliberately left. The two conventions no longer meet inside a
  query: the cascade resolvers projected uuid chains purely to match these three tables, and each chain
  now projects the **name** while still recursing on `parent_id`. Proven by `TestOwnerArcsSurviveARename`,
  which binds a tag, a variable, and a secret at a location, renames it, and asserts all three still
  resolve; **mutation-checked**, since removing `on update cascade` makes the rename fail outright rather
  than merely orphan the rows. `tag_binding.node_id` keeps its uuid (a node is addressed by its enrollment
  identity) and `tag_binding.tag_id` is tracked separately in
  [#340](https://github.com/hyperscaleav/omniglass/issues/340).
