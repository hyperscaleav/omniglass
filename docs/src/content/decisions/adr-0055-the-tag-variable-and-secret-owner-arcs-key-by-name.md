---
title: "The tag, variable, and secret owner arcs key by name"
date: 2026-07-21
status: "Superseded by [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key)"
summary: "The tag, variable, and secret owner arcs key by name"
---

- **Date:** 2026-07-21 | **Status:** Superseded by [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key), which
  converts these nine columns back to uuids along with every other name-keyed foreign key. Kept in
  full because the reasoning below is a worked example of the mistake: it is internally consistent,
  it shipped green, and it is wrong at the premise.
- **Context:** [ADR-0053](#adr-0053-a-name-is-the-address-a-uuid-is-identity) fixed what operators saw
  and deliberately left the schema alone. Three tables still keyed their owner arcs by uuid while every
  table from the collection era onward keyed by name, so the two conventions met inside single queries:
  the cascade resolvers walked chains of **uuids** purely to match these three, and a component's name
  had to be carried alongside its id to bridge them.
- **Decision:** the nine arc columns on `tag_binding`, `variable`, and `secret` become `text references
  <entity> (name) on update cascade on delete cascade`. The columns keep their `_id` suffix, matching
  `role_assignment.component_id` and `state_datapoint.component_id`, which are likewise text referencing
  a name.
- **`on update cascade` is the load-bearing clause.** A name is only safe as a key if a rename carries.
  `TestOwnerArcsSurviveARename` is the proof and is **mutation-checked**: with the clause removed the
  rename is refused outright with a foreign-key violation, so the test cannot pass vacuously. It also
  could not have failed before this change, since the arcs held uuids that a rename never touches.
- **The resolvers now project names.** Each chain still **recurses on `parent_id`**, which stays a uuid,
  and only what it projects changed. `owner_id` in the `owners` CTE is a name, so the final joins that
  resolve a display name match on `name` rather than `id`.
- **The scope walk still uses ids**, resolved from the name at the point of the check. Identity stays
  internal, and the walk is the only place that needs it, which is the rule working as intended rather
  than an exception to it.
- **Not converted, deliberately:** `tag_binding.node_id` references `node.principal_id`, a node's
  enrollment identity and the only handle it has. `tag_binding.tag_id` is a genuine instance of the same
  rule (`tag` is uuid-keyed with a unique name) but is the binding's **subject** rather than its owner,
  and it touches the tag CRUD surface rather than resolution; tracked as
  [#340](https://github.com/hyperscaleav/omniglass/issues/340). Removing it from the guard test's
  slug-keyed allow-list, where it had been listed on a **false claim** that the tag catalog is
  slug-keyed, is part of this change.
- **No `migrate:down`.** Reversing would have to resolve names back to uuids the forward migration no
  longer records, and any rename since would make that resolution wrong rather than merely absent.
- **Tracked as** [#339](https://github.com/hyperscaleav/omniglass/issues/339).
