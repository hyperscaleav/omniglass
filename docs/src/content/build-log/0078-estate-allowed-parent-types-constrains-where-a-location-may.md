---
title: "Estate: `allowed_parent_types` constrains where a location may be placed"
date: 2026-07-14
refs: []
form: bullet
---

- **Estate: `allowed_parent_types` constrains where a location may be placed.** `location_type`
  gains `allowed_parent_types` (`text[]`, default `{}`, a new idempotent migration): a set of
  `location_type` ids and/or the reserved `root` sentinel a location of that type may sit under.
  Empty is unconstrained (every existing custom type, unchanged); non-empty is enforced on
  `CreateLocation` and a new move primitive on `UpdateLocation` (a `parent` patch field,
  cycle-guarded). A violation is a distinct `storage.PlacementError` (wraps
  `storage.ErrPlacementNotAllowed`, carries the offending child and parent type names), mapped to
  a 422 that names both. `CreateLocationType` refuses the id `root`, so a real type can never
  collide with the sentinel. The four seeded types ship their sets: `campus={root}`,
  `building={root,campus}`, `floor={building,campus}`, `room={floor,building,campus}`. The
  [Types catalog](/guides/admin/types/) gains an allowed-parents editor (a checkbox list over the
  location types plus a Root option) on the location tab's create and edit forms, and the
  **location edit form's Placement section makes Parent editable**: a picker (riding #240's
  inventory edit model, the same `Show when={editing()}` split as every other editable field)
  narrowed to the type's allowed parents and excluding the location's own subtree, wired to the
  move primitive; moving back to root is not offered
  ([ADR-0030](/architecture/decisions/#adr-0030-allowed_parent_types-constrains-where-a-location-may-be-placed),
  [#239](https://github.com/hyperscaleav/omniglass/issues/239)). Proven by a storage integration
  suite (unconstrained allows all, a listed parent and root both succeed, an out-of-order
  placement is refused on create and on move with the type names in the error, a cycle move is
  refused distinctly, and an existing noncompliant placement is untouched by an unrelated update
  until something tries to move it), a boot-seed idempotency assertion, and API/web tests
  (including the reparent picker's candidate filtering and self-exclusion). Deferred:
  `allowed_parent_types` for `system_type`/`component_type`, the same read-only-in-edit Parent gap
  on Systems and Components, and a drag-to-reparent affordance (this slice's picker is a select,
  not a drag surface).
