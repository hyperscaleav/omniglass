---
title: "Field: batched edit, upsert, and inherited-versus-set clarity"
date: 2026-07-19
refs: []
form: bullet
---

- **Field: batched edit, upsert, and inherited-versus-set clarity.** The Fields panel's edit flow is
  reworked. Setting a value is now an **idempotent upsert** (`SetFieldValue`): the first set creates, a
  later set patches in place (no more `409 field already exists` on a second set), and a set to the
  unchanged value is a no-op, so the `set-field-value` route returns `200` and stays `field:create`-gated.
  In the UI the per-field save is gone: a field's edit **stages a draft** and the blade's **Save changes**
  flushes every touched field (an upsert, or a delete for a cleared override) alongside the component core,
  through a new `onSave` contributor on the blade edit slot. Edit mode now tells **inherited** from
  **set**: an inherited field is an empty input with a greyed `unset` placeholder, a set field shows its
  value with a **clear (×)** that stages a revert to the type default (replacing the revert arrow). Proven
  by a storage integration test (a second set patches in place, a same-value set writes no audit row), the
  updated HTTP e2e (the second POST upserts to `200` keeping its `value_id`), and a blade-slot unit test
  (contributors flush before the primary save, and a throwing contributor aborts and keeps the blade in
  edit). The [config, secrets, and variables](/architecture/variables/) page keeps its `Partial` field
  badge.
