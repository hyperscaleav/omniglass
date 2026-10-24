---
title: "`capability` and `standard` take uuid primary keys"
date: 2026-07-22
refs: []
form: bullet
---

- **`capability` and `standard` take uuid primary keys** (slice 2 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
  their kebab id becomes `name`, and eight inbound foreign keys move to the uuid, `capability` from
  `product_capability`, `role_capability`, `component_capability`, and `alarm_capability`; `standard` from
  `standard_property`, `system_role`, its own `parent_standard_id`, and `system.standard_id`. Every read
  that returned a capability set (the effective-capabilities resolver, the role requirement lists, an
  alarm's degraded set, the health rollup) now projects the capability's **name**, so the wire surface is
  unchanged while the storage key is a uuid. Two migration facts from slice 1 recurred and are worth the
  note: a renamed table keeps its old primary-key constraint name (here `standard` still answered to
  `system_type_pkey`), so the drop looks the name up from the catalog rather than guessing; and recreating
  a column drops the NOT NULL and the unique constraints it carried, so an unknown capability that resolves
  to null on a not-null arc is mapped back to the ref-not-found sentinel rather than surfacing as a 500.
  The rename test now renames a capability and a standard under a live product, role, and contract and
  asserts each still resolves and reads the new handle.
