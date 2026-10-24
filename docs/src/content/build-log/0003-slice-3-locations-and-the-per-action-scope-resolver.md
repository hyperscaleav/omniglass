---
title: "Slice 3: locations and the per-action scope resolver"
date: 2026-06-28
refs: []
form: bullet
---

- **Slice 3: locations and the per-action scope resolver.** The first scoped [core entity](/architecture/core-entities/),
  and where the [ABAC scope](/architecture/identity-access/) deferred from Slice 2 lands. The `location_type`
  registry (the only shape-definer, since a location has no template) seeds the four official types at boot,
  ranked (campus/building/floor/room, spaced by ten); `location` is a name-addressable, variable-depth tree
  (`parent_id`) whose type is a foreign key. `POST/GET/PATCH/DELETE/list /api/v1/locations` each declare a
  `location:<action>` capability and resolve the caller's per-action `visible_set` from their grants and the
  role index, which the [Storage Gateway](/architecture/storage/) expands (a recursive subtree walk) into the
  row filter and applies on every query, writing the `audit_log` row in the same transaction. The over-permit
  fix is real: only the grants whose role carries the action contribute scope, so a read-anywhere principal
  with a narrow write grant cannot write outside it. The three-way status split holds: out of read scope is a
  non-disclosing 404, readable-but-out-of-action-scope is 403, and a delete is refused (409) while the location
  still has children. The `location_type` registry that classifies a location is itself readable at `GET /api/v1/types/location` (ranked, gated by `location:read`), which the operator console type picker lists. Proven by a pure resolver unit suite, a testcontainer integration test of the gateway
  split and audit, and an end-to-end HTTP test (scoped subtree listing, the 404, the capability 403, full owner
  CRUD). Deliberate thin cuts: scope kinds resolved are `all` and `location` only; the `location_type` registry
  seeds official rows only; `rank` orders and signals hierarchy but does not constrain nesting; update patches
  `display_name` and `location_type` (rename and reparent deferred); occupancy counts child locations only
  (placed systems and components join when they land). Deferred: system and component entities (Slice 4),
  group and dynamic-group scope (Slice 5), operator-defined location types via the namespace shadow, hard
  containment rules, and any `LocationTemplate`.
