---
title: "Slice 4b: the component tier, and a generic scoped-CRUD primitive"
date: 2026-06-29
refs: []
form: bullet
---

- **Slice 4b: the component tier, and a generic scoped-CRUD primitive.** The estate is complete: `component`
  (the leaf) joins locations and systems, with a `component_type` registry and a tree that belongs to a
  primary `system_id` and is located at a `location_id`. The per-entity boilerplate is now a **shared
  generic** (`internal/storage/scopedcrud.go`): the read, resolve, and delete paths (list/get/resolve-for-action/
  delete-with-occupancy/by-name, all scope-aware and audited) are written once over a `scopedConfig[T]`, and
  locations and systems were refactored onto it; each entity keeps only its own create/update (which differ by
  the foreign keys they resolve). So a new scoped entity is a registry, a thin create/update, and a route file,
  not a mirrored copy. And the [authorization conformance suite](/architecture/identity-access/) made adding
  component **one registry line**: the full security matrix (capability 403, the over-permit scope 403, the
  non-disclosing 404, in-scope success) and the no-unguarded-route guard now cover all three entities
  automatically. Proven by the pure resolver `component` case, a testcontainer integration test (subtree scope,
  the split, occupancy, belongs-to + located-at FK resolution, faults, audit), the generic authz conformance
  over location + system + component, and the generated `omniglass component` CLI. Thin cuts as the other
  tiers: own-subtree scope (no cross-tier cascade), reparent/rebind/relocate deferred, components UI deferred.
