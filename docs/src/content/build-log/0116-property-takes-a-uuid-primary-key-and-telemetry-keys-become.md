---
title: "`property` takes a uuid primary key, and telemetry keys become real foreign keys"
date: 2026-07-22
refs: []
form: bullet
---

- **`property` takes a uuid primary key, and telemetry keys become real foreign keys** (slice 3 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
  the largest registry conversion. `property`'s kebab id becomes `name`, and its **seven** references
  move to the uuid: four contract/value tables (`product_property`, `standard_property`,
  `location_type_property`, `property_value`) that already referenced it, plus the `key` column on
  `metric_datapoint`, `state_datapoint`, and `event`, which had been a loose property name with **no**
  foreign key. Those three become real `property_id` foreign keys, so a rename now follows an observation
  series, not only a contract. Every telemetry key was already a registered property (reject-not-project
  drops an unregistered name at collection), so the constraint only makes the invariant the database's as
  well. Every read still projects the property's **name** as `key` and `property_name`, so the wire
  surface is unchanged. Proven by `TestRegistryHandleRenameKeepsReferences`, extended to rename a property
  under a live contract, a declared value, and a metric datapoint, and assert the datapoint reads the new
  key. The remaining four leaf registries are slice 4.
