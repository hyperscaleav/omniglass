---
title: "A label rule renders a label, over a data map that is the sandbox"
date: 2026-08-11
refs: []
form: bullet
---

- **A label rule renders a label, over a data map that is the sandbox**
  ([#682](https://github.com/hyperscaleav/omniglass/issues/682), the second slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0098](/architecture/decisions/#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)).
  `display_name` gains the pen `display_name_generated` on component, system and location, the shape
  `name_generated` already had, and a nullable `label_rule` lands on `component_type`, `system_type`,
  `location_type`, `product` and `standard` with the global tier in its own small table. Rules
  resolve most-specific-first and inherit down a nested registry by the same first-non-null walk
  `stem` and `abbrev` follow, per node across the fork boundary, so setting a rule on a shipped type
  forks that row and the official one stays byte-identical.

  The rule language is Go `text/template`, and a custom interpolation DSL was designed and rejected,
  because the security property a label rule needs is not a syntax that cannot express dangerous
  things but an environment that contains nothing dangerous. The environment is a
  `map[string]string` we build key by key: field traversal, a method call and `call` all fail at
  execution over a string, `index` reaches only what the map already holds, an undefined key renders
  as nothing, and a secret is absent structurally rather than filtered. The test that tries to reach
  one pins the key set exactly and then asks for every word a credential might be called, over a
  database holding a real sealed secret.

  What the map deliberately omits is placement, and that is the slice's one divergence from how the
  definition's acceptance text could be read. Because the label is stored, every input to a rule is
  something a write path must re-render on, and the keys chosen change on exactly five acts, all of
  them the entity's own: create, rename, move, reclassify, reset. Adding a location's name would add
  a sixth that is somebody else's, staling every label under a renamed room, and the
  recompute-and-compare invariant would be right to fail. The cascade that would make placement facts
  safe belongs with the bulk recompute ([#685](https://github.com/hyperscaleav/omniglass/issues/685)),
  which can add the keys and the cascade in one piece.

  The global tier is one row per entity kind with **two** columns, `default_template` for what the
  release ships and `template` for what the operator chose, because one column cannot be both
  authoritatively re-seeded and operator-owned. The location kind ships an empty rule on purpose: a
  campus, a building and a room have real-world names, and every fact a rule could reach is either
  the type ("Room", for every room) or the name itself, so any rule that ran would be a constant or a
  restatement, and the read ladder's fallback to the name is already the better answer. That is the
  same argument the epic makes for refusing to auto-name a nominal location type.

  Twenty-three mutations back the assertions, and two of them were surviving at first for the reason
  slice 1 warned about. The empty-render test asserted a `NULL` column after a create whose rule
  produced nothing, but the stamp skips a write when the render equals what is stored, so nothing was
  written and the assertion held whatever the write would have done; it now drives a labelled row
  into a rule that stops producing one. And the recompute-and-compare invariant did not notice a
  `:move` that fails to re-render, because its fixture moved a component into a bucket where the same
  ordinal was free, so name, number and label were all unchanged; the bucket is now stocked to force
  a re-mint, and its reset is preceded by a rename for the same reason.
  Review found four things after the slice was first proposed, and one was a hole in the argument
  rather than a bug in the code. **A ceiling on output is not a ceiling on work.** The rendered-label
  cap bounds bytes reaching the writer, and a value built inside a pipeline is materialized by `fmt`
  and never written, so the cap never sees it: 437 bytes of operator-authored rule allocates 85 MB
  and writes 8, every further doubling is 35 more bytes of rule for twice the memory, and twenty of
  them is an unrecoverable OOM of the single binary that re-triggers on every later write to any
  entity of that type, because the rule is stored and storing it on a shipped type forks that row for
  everything under it. Refusing `:=` does not fix it (the nested-pipeline form needs no assignment)
  and neither does removing a FuncMap entry (`printf` is a builtin the FuncMap never granted). The
  fix is the closed-data-map argument applied to the other half of the sandbox: an **allowlist over
  the parsed tree**, a closed set of node types and function names, refused at parse time so nothing
  is ever stored. Six mutations back it, including a forbidden call in non-first argument position
  behind an allowed function, which is the form a checker that inspects only the outermost call would
  wave through.

  The other three were the same defect in three places, a claim with nothing proving it. The pen
  defaulted false with no backfill, so on an upgraded estate the feature would have applied only to
  rows created afterwards; three of the four non-component stamps could be deleted with the suite
  green, because every rename test ran against rows whose rule read nothing a rename changes and the
  recompute-and-compare invariant iterated components only; and `label_rule` was declared
  `ShapeIDOnly`, which promises a uuid, against a text primary key with no id column, published into
  a generated reference. All three now have a test that fails when the thing is removed, and the
  shape guard was extended from "every table has a declaration" to "the declaration agrees with the
  primary key", which is what makes the third one catchable at all.
