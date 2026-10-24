---
title: "Slice 4a: the system tier"
date: 2026-06-29
refs: []
form: bullet
---

- **Slice 4a: the system tier.** The second scoped [core entity](/architecture/core-entities/): a `system_type`
  registry (seeded official, ranked, like `location_type`) and `system` as a name-addressable, variable-depth
  tree (`parent_id` subsystems), optionally located at a location (`location_id`) and classified by its type.
  Full scoped CRUD (`POST/GET/PATCH/DELETE/list /api/v1/systems`), gated by `system:<action>`, with the same
  per-action `visible_set`, three-way 404/403 split, and in-transaction audit as locations. The scoped-tree
  recursive walk is now a **shared primitive** (`internal/storage/scopetree.go`): locations were refactored
  onto it and systems consume it, so the over-permit-safe subtree filter is written once. Proven by the pure
  resolver unit cases for the `system` kind, a testcontainer integration test (subtree scope, the 404/403
  split, occupancy, FK faults, located-at, audit), and an end-to-end HTTP test plus the generated
  `omniglass system` CLI. Deliberate thin cuts: each entity is scoped by its **own** subtree (the cross-tier
  cascade, a location scope also covering its systems, is a later slice); reparent and located-at editing are
  deferred (create-time only). Deferred: components (Slice 4b), the cascade, templates, datapoints, and the
  systems UI (console section stays a stub for now).
