---
title: "`allowed_parent_types` constrains where a location may be placed"
date: 2026-07-14
status: "Accepted"
summary: "`allowed_parent_types` constrains where a location may be placed"
---

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [core-entities](/architecture/core-entities/), [Types guide](/guides/admin/types/), [Work with an entity](/guides/operator/entities/)
- **Decision:** `location_type` gains `allowed_parent_types` (`text[]`, default `{}`): a set whose
  members are `location_type` ids and/or the reserved `root` sentinel (a placement at the top,
  no parent). An empty set is unconstrained (the default, and every existing custom type until an
  operator opts in); a non-empty set is enforced: a placement is valid iff the parent is null and
  the set contains `root`, or the parent location's type is in the set. `root` cannot collide with
  a real type id: `CreateLocationType` refuses it. Enforcement is forward-only, on `CreateLocation`
  and the location move path (`UpdateLocation`'s new `ParentName` patch field, added this slice so
  the "grandfathered until moved" guarantee is real and testable, not merely a claim); an existing
  placement a type's set no longer allows is untouched until something tries to move it. The four
  seeded types get their sets: `campus={root}`, `building={root,campus}`,
  `floor={building,campus}`, `room={floor,building,campus}`. Re-parent ships operator-usable this
  slice: the location edit form's Placement section makes Parent editable, a picker built on
  #240's inventory edit model (the same `Show when={editing()}` field/fact split every other
  editable field on the accordion uses), narrowed to the set and excluding the location's own
  subtree; moving back to root is not offered (the move primitive does not support it this slice).
- **Context:** `rank` ([ADR-0028](/architecture/decisions/#adr-0028-rank-retired-from-the-type-registries-sort-is-alphabetical))
  was sort-only and never expressed the estate's real hierarchy rule (a floor does not belong
  above a room). A `child.level > parent.level` rule was rejected: it does not generalize past
  locations (systems and components have no total order), while a type-level allowed-parent set
  expresses both the general "may skip a level" case and the specific "may never be root" or
  "may never nest under this particular type" cases with one field. A separate `root_placeable`
  boolean was rejected in favor of folding root into the set as a sentinel, keeping one field and
  one validation path. Enforcing retroactively was rejected: seeding a type's set must never
  invalidate an existing estate. Locations had no move or re-parent capability at all before this
  slice (create-time placement only); the storage/API primitive was originally scoped without a UI
  trigger (the console's placement fields render read-only in every edit context today, on all
  three inventory pages), but the decision changed once #240's create-as-route edit model landed
  as the concrete field pattern to hang a reparent picker off: one PR ships the enforcement point
  and a real way to use it, rather than a primitive an operator cannot reach. The picker's
  candidate list is narrowed client-side (a UX nicety); the server-side `validatePlacement` call
  is the actual gate, so a stale or bypassed client filter still gets an inline 422, not a
  silently-accepted violation. One divergence from the design surfaced while building the move
  primitive: `UpdateLocation` checks placement before the cycle guard, not after, so a move that is
  simultaneously a placement violation and a structural cycle (moving a location under its own
  descendant, where the descendant's type also does not allow this child) reports the `PlacementError`
  (422, naming both types) rather than the generic `ErrLocationCycle`; the design left the check
  order unstated, and the more specific, actionable error was chosen to win. Systems and components
  lose `rank` too but get no `allowed_parent_types` this slice, and keep their existing
  read-only-in-edit Parent field: a leaf or must-nest constraint there is closer to a boolean than
  an ordered set, deferred until a concrete need names the shape, and extending the same
  editable-Parent pattern to two more pages is a follow-up, not bundled here.
- **Closes:** issue [#239](https://github.com/hyperscaleav/omniglass/issues/239). Design:
  `docs/superpowers/specs/2026-07-14-type-placement-constraints-design.md`.
