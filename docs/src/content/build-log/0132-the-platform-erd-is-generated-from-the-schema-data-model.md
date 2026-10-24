---
title: "The platform ERD is generated from the schema ([data model](/architecture/data-model/))"
date: 2026-07-28
refs: []
form: bullet
---

- **The platform ERD is generated from the schema ([data model](/architecture/data-model/)).** A new
  `make gen` step (`cmd/erdgen`) applies the embedded migrations to a throwaway Postgres, introspects
  `information_schema` (tables, primary and foreign keys), and renders a subsystem-clustered D2
  entity-relationship diagram onto the new **Data model** page, so the diagram can never drift from
  the tables. The `internal/erd` render core is pure and golden-tested; the introspection and a drift
  gate (regenerate, compare to the committed D2) run against the real schema. Tables land in
  hand-mapped subsystem containers (identity, estate, catalog, telemetry, collection, config, content,
  audit); an unmapped table renders in a visible `unclustered` box that trips the drift gate. The
  sql_table shapes are themed from the brand tokens so the ERD tracks the light/dark toggle. Part of
  [#406](https://github.com/hyperscaleav/omniglass/issues/406).
