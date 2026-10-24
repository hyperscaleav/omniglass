---
title: "Placement and classification are mutable after create"
date: 2026-07-23
status: "Accepted"
summary: "Placement and classification are mutable after create"
---

- **Date:** 2026-07-23 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [api](/architecture/api/)
- **Decision:** a component's **product**, **location**, and **parent**, and a system's **location**
  and **parent**, are patchable after create, not fixed at creation. Each follows the house
  **three-state** convention on `PATCH`: an omitted field is unchanged, an explicit empty string
  **clears** (a productless one-off, an unplaced entity, a root), and a name **sets**. A re-parent is
  **cycle-guarded** (refused when the new parent is the entity itself or one of its own descendants,
  the same recursive walk `location` already uses) and **scope-injected** (the new parent must sit
  inside the caller's update scope). The existing per-transaction audit and health recompute rules are
  unchanged.
- **Context:** the create body accepted these fields, the update body did not, so a component
  classified wrong on import or a display that physically moved rooms had **no path back except delete
  and recreate**, which destroys its telemetry history. The gap was invisible from any single body; it
  only showed up as the set difference between the create and update schemas, which is what surfaced it
  ([#342](https://github.com/hyperscaleav/omniglass/issues/342)).
- **Why product is the sharp one:** [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value)
  made `product` the carrier of the property contract, so a wrong product resolves the wrong property
  defaults. A swap therefore keeps every explicitly-set value (they key by component and property_type,
  independent of product) and lets only the unset defaults follow the new product, so re-classifying is
  never a silent data loss.
- **What is deliberately NOT carried over from the location pattern:** location's reparent also runs an
  **allowed-parent-type** placement check, because a `location_type` constrains its parents. Products
  and standards are not placement-typed, so a component and a system carry the **cycle guard and scope
  injection only**, no placement-type validation. A component and a system reparent also, unlike a
  location this slice, support clearing to a root, since a root component and a root system are ordinary.
- **Health does not move on a placement change.** The rollup runs component to systems-it-staffs to
  locations-over-those-systems, so a component's own location and parent, and a system's parent, sit
  outside the chain; only a product swap (capabilities) and a system relocate (which rung it rolls into)
  recompute, both already wired.
- **Storage was already half-built:** `ComponentPatch.ProductName` and `SystemPatch.LocationName` were
  wired but unexposed; this slice adds the two reparent paths and the component relocate, and opens all
  of it on the API. It unblocks the [CRUD form primitive](/contributing/design-system/), whose generated
  edit form reads mutability off the create-minus-update schema difference and would otherwise render
  these fields read-only.
