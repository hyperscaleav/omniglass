---
title: "`rank` retired from the type registries; sort is alphabetical"
date: 2026-07-14
status: "Accepted"
summary: "`rank` is dropped from `location_type`, `system_type`, and `component_type`; the three list operations sort by `display_name, id` instead"
---

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [core-entities](/architecture/core-entities/), [Types guide](/guides/admin/types/)
- **Decision:** `rank` is dropped from `location_type`, `system_type`, and `component_type`: the
  column (a new idempotent migration), the three API bodies and create/update inputs, the
  boot-seed YAMLs, the generated client and CLI, and the Types catalog page (no Rank column, no
  Rank field on create or edit). `ListLocationTypes`, `ListSystemTypes`, and `ListComponentTypes`
  now order by `display_name, id` instead.
- **Context:** `rank` was sort-only from the start (the location_type seed comment already said
  so: "rank does NOT constrain nesting"), never an enforcement mechanism. The upcoming
  `allowed_parent_types` placement constraint on `location_type` needed a clean field to
  introduce without a stale, unused ordering column sitting beside it, so retiring `rank` is the
  mechanical precursor to that slice rather than part of it: this PR only removes the field and
  switches the sort, `allowed_parent_types` is a separate slice. Alphabetical is the obvious
  default with no enforcement semantics to preserve; an operator who wants a specific browse
  order can still rely on the id or display name they chose.
- **Closes:** part of issue [#239](https://github.com/hyperscaleav/omniglass/issues/239) (the
  `allowed_parent_types` half continues in a follow-up PR against the same issue). Design:
  `docs/superpowers/specs/2026-07-14-type-placement-constraints-design.md`.
