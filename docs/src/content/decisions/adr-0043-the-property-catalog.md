---
title: "The property catalog"
date: 2026-07-19
status: "Accepted"
summary: "The `datapoint_type` catalog is generalized into a primitive-agnostic **`property`** catalog (the typed set of signals a datapoint observes and a field declares): the unused scope ladder becomes an `official` boolean, `value_type` becomes `data_type` (text to string, add bool), `kind` is nullable (a declared-only property has none), and `validation` is a **JSON Schema** validated by Huma's own validator (zero new dependencies). Value/source tables key by **name** (no FK), so the rename is behavior-preserving; the type-schema (`field_definition.key`) is the only binding and lands in PR-B"
---

- **Date:** 2026-07-19 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** The `datapoint_type` catalog is generalized into a primitive-agnostic **`property`**
  catalog: one typed catalog whose entry (a **property**) is a canonical, typed name that a datapoint
  **observes** and a field **declares**, identified by a **`key`** (its canonical name). The physical
  table is `property`; the concept, the API resource (`/properties`), the Go `Property` type, and the
  console all read `property`, while a property's identifier (its `key`, and the `field_definition.key`
  reference) stays `key`. Four shape changes fold `datapoint_type` in: the unused `(scope, name)` ladder
  (`org`/`template` never had an operator write path, `template_id` was a dangling column) collapses to a
  `name` primary key plus an **`official`** boolean (seed-owned rows are read-only); `value_type` becomes
  **`data_type`** over the unified set `{string, int, float, bool, json}` (`text` backfills to `string`,
  `bool` is added); **`kind`** (metric/state/log) becomes nullable, since a declared-only attribute
  property has no observed kind; and **`validation`** is a **JSON Schema** fragment (`pattern`/`enum`/`minimum`/
  `maximum` and, for a json-typed property, a nested object schema), enforced by Huma's own validator with the stored
  schema loaded through `yaml.v3`, so there is **no new dependency**. Value and source tables continue to
  key by the **name string** (no foreign key, reject-not-project at ingest exactly as before), so the
  rename is behavior-preserving: the collection registry, the reachability BFF, and the metric/state sinks
  keep working unchanged.
- **Context:** Datapoints already had a typed canonical-key catalog (`datapoint_type`: name, value_type,
  display_name, unit, validation, kind), while fields, variables, secrets, and tags had no catalog at all,
  an operator typed a key and it was registered nowhere. Rather than build a parallel table, this slice
  makes the one catalog primitive-agnostic so `serial_number` is the same concept whether a device reports
  it (observed, a datapoint) or an operator types it (declared, a field). The `official` boolean chassis is
  chosen over finishing `datapoint_type`'s never-built scope-shadow precedence: it is the proven,
  finished model the `*_type` registries already use.
- **Values reference the key, not a `<primitive>_key` layer:** the only binding is the type-schema
  (`field_definition` gaining a `key` reference so a field draws from the catalog), which is **PR-B**, not
  this slice. Provenance rides the value (an observed metric versus a declared field value share the key),
  so reconciliation of declared-versus-observed sources needs no middle table; it is deferred.
- **Deferred:** the `field_definition.key` reference (PR-B); the type-schema editor (how a `component_type`
  selects properties); reconciliation (the declared-versus-observed drift signal); a console editor for the
  validation JSON Schema (set via the API for now); variables/secrets/commands/tags adopting a `key`
  reference; and an operator shadow of an official property.
- **Closes:** issue [#297](https://github.com/hyperscaleav/omniglass/issues/297) (the field catalog,
  expanded to the property catalog), under epic
  [#266](https://github.com/hyperscaleav/omniglass/issues/266).
