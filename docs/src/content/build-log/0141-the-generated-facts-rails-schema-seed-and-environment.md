---
title: "The generated-facts rails: schema, seed, and environment claims can no longer drift ([#433](https://github.com/hyperscaleav/omniglass/issues/433))"
date: 2026-08-01
refs: [433]
form: bullet
---

- **The generated-facts rails: schema, seed, and environment claims can no longer drift
  ([#433](https://github.com/hyperscaleav/omniglass/issues/433)).** Three artifacts now render the fact classes the
  2026-07-30 audit found drifting worst, each committed by `make gen`, gated by `gen-drift.yml`, and pinned by its
  own drift test. `schema.json` (erdgen's introspection, extended to nullability, defaults, CHECK constraints, and
  unique indexes) feeds the `SchemaTable` component, and eleven architecture pages plus [tags](/architecture/tags/)
  now render their storage-table mechanics from it, hand-written narrative kept as notes; the sweep verifies no
  remaining hand-written row claims a live table's columns. `seed.json` (seedgen, no database: the twelve embedded
  seed YAMLs, with role rows carrying **effective** permissions through the same rbac path the authorizer uses)
  feeds `SeededSet`, and five admin guides render their shipped-set claims from it, retiring the enumerations that
  undercounted the property catalog. `config.json` (configgen) renders the new **declarative environment
  registry**: every variable the binary reads is declared with its doc and scope, `config.Get` refuses an
  undeclared key, and the container and Helm guides render their env tables from it (the Helm guide previously
  documented none). Two API guards land with it: every gated operation's description must name its
  `x-omniglass-permission` stamp verbatim (one shipped deviation corrected: `principal:purge:admin`), and api.md's
  last hand-written route table is replaced by the generated `/reference/api/`. Built as the
  [#502](https://github.com/hyperscaleav/omniglass/issues/502) pilot of the feature-loop framework; the run's
  lessons landed on [feature loops](/contributing/feature-loops/) in the same PR.
