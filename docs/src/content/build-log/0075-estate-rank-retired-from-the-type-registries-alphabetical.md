---
title: "Estate: `rank` retired from the type registries; alphabetical sort"
date: 2026-07-14
refs: []
form: bullet
---

- **Estate: `rank` retired from the type registries; alphabetical sort.** `rank` was sort-only
  (no nesting enforcement, per its own seed comment) on `location_type`, `system_type`, and
  `component_type`; a new dbmate migration (`ALTER TABLE ... DROP COLUMN IF EXISTS rank`,
  idempotent) drops it from all three tables, and `ListLocationTypes`/`ListSystemTypes`/
  `ListComponentTypes` now `ORDER BY display_name, id`. The field is gone from the three type
  bodies, the boot-seed YAMLs, the generated client and CLI, and the
  [Types catalog](/guides/admin/types/) page (no Rank column, no Rank field on create or edit).
  This is the mechanical precursor to `allowed_parent_types` (placement constraints on
  `location_type`), which lands as its own slice
  ([#239](https://github.com/hyperscaleav/omniglass/issues/239)). Proven by the storage
  round-trip per registry (alphabetical order, not insertion or id order), the boot-seed
  idempotency test (the lowest-alphabetical official row per registry), and a web test suite
  update (no Rank fixtures, no Rank assertions).
