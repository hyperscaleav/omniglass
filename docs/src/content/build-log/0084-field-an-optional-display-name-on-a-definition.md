---
title: "Field: an optional `display_name` on a definition"
date: 2026-07-17
refs: []
form: bullet
---

- **Field: an optional `display_name` on a definition.** A `field_definition` gains a nullable
  **`display_name`**, a human label threaded schema through UI: the console shows the label wherever it is
  set (the Fields editor on the component-type blade and the **Effective fields** panel), while the raw
  `name` stays the unique key and the interpolation handle, so an unset label falls back to the key. The
  column is additive and idempotent, presentation only. Proven by the storage round-trip (the definition
  create and update carry the label, an unset label stores NULL), the HTTP e2e (the create body accepts
  `display_name` and it round-trips through the catalog list), and the dev seed (the seeded display fields
  carry labels without changing row counts). The [config, secrets, and variables](/architecture/variables/)
  page keeps its `Partial` field badge.
