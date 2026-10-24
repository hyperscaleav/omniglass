---
title: "The name foundation of [ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables) lands: `property_type`, `property`, `property_type_id`"
date: 2026-07-23
refs: []
form: bullet
---

- **The name foundation of [ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables) lands: `property_type`, `property`, `property_type_id`.** The
  signal-definition registry (long carried in the docs as `datapoint_type`, and built as `property`)
  becomes **`property_type`**; the latest-value store `property_value` takes the freed bare noun
  **`property`**; and every data table's FK to the registry (`metric`, `state`, `event`,
  `product_property`, `standard_property`, `location_type_property`, and `property` itself) is repointed
  `property_id` to **`property_type_id`**, on the wire as well as in the column. A guarded dbmate
  migration does the table and column renames (the stale dependent object names are left for a cosmetic
  pass, as the collapse did); the storage structs (`PropertyType`, `Property`), the API bodies, the
  generated OpenAPI, typed client, CLI, and JSONSchema, and the console all follow. This closes the
  `datapoint_type`-to-`property` divergence the docs carried: the registry is `property_type`
  everywhere. The rename is **complete, not a thin cut**: the public resource surface moves too (the
  registry is `GET/POST /property-types` under the `property_type:*` permission, while the owner-scoped
  `/{owner}/properties` value routes stay), the both-forms name pair reads `(property_type_id,
  property_type_name)`, the audit resources are `property_type` / `property`, and a second migration
  block renames every dependent constraint and index in line with its table. The fuller ADR-0063 model
  (the provenance-keyed cache, the `event_type` family, the `command` pillar) is still staged.
