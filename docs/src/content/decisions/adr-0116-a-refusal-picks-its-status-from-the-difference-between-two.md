---
title: "A refusal picks its status from the difference between two scopes"
date: 2026-08-14
status: "Accepted"
summary: "A refusal on a targeted mutation picks its status from the **difference between the read scope and the action scope**, not from one set: outside `visible_set(P, read)` stays the non-disclosing **404**, and inside the read scope but outside `visible_set(P, action)` is a **403** that names the missing authority. Acknowledging existence to a caller who can already `GET` the row discloses nothing it does not have, and the 404 it replaces was a statement about existence that the caller could see was false. The **condition** that makes it safe is that the read set is the caller's own `<resource>:read` and never a wider convenient one; a route checking a wider set would hand the existence of a row to somebody with no grant to see it, which is the leak the 404 exists to prevent. This was already the contract for the three tree entities' own CRUD; #736 is its adoption by the **seventeen routes that hang off a tree entity** (property set/clear on all three tiers, system membership and role writes, component alarm writes), through the existing `resolveScoped` rather than a second refusal helper, plus one owner-generic seam (`ResolveActionTarget`) for the routes whose write takes no scope of its own. Two carve-outs hold: `resolvePlacementRef`'s CROSS-TIER reference keeps its non-disclosing not-found ([ADR-0107](#adr-0107-a-component-creates-system-bind-is-a-system-write)), and the estate-wide resources (principal, group, tag registry, node) are all-or-nothing and already 403. The **conformance matrix** asserts all three branches per ROUTE, each entity declaring the routes that hang off it, because those routes are not uniform across the registry"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A targeted mutation resolves its target **twice**: within the caller's
  `<resource>:read` scope, then within the action's own scope. Outside the read scope the answer is
  the non-disclosing **404** it always was. Inside the read scope and outside the action scope the
  answer is **403**. The status is the difference between the two sets, not a property of either one.
- **Why acknowledging existence here discloses nothing.** The caller can `GET` the row. A 403 that
  says "you may not do that here" hands them a fact they already hold, while the 404 it replaces
  makes a claim about existence that the same caller can immediately disprove with a read. That is
  worse than uninformative: an operator looking straight at a component in the console, told it does
  not exist, reads it as a broken platform and files a bug against the wrong thing, which is exactly
  what [#728](https://github.com/hyperscaleav/omniglass/issues/728) did.
- **The condition, which is not a convention.** The read set each route passes must be the caller's
  own `<resource>:read`. The 403 names a row, so it may only ever be reached by a caller who could
  have read that row anyway; a route that checked a wider set (the action's own, a neighbouring
  tier's, a convenient all-scope) would disclose the existence of a row to somebody holding no grant
  to see it, which is the precise leak the non-disclosing 404 exists to prevent. This is the one
  thing to check when adopting the split on a new route.
- **Adoption of an existing primitive, not a new one.** `resolveScoped` has made this split for the
  three tree entities' own CRUD since it was written. What answered 404 for the readable case was the
  seventeen routes that hang **off** a tree entity and resolve it as their target: property set and
  clear on component, system and location; system membership add, remove and setPrimary; system role
  declare, withdraw, assign, unassign and swapPositions; component alarm raise, clear and
  acknowledge. Each now routes through the same primitive, via an owner-generic seam
  (`resolveTargetID`, exported as `ResolveActionTarget`) for the ones whose gateway write takes no
  scope of its own. A second refusal helper would have been the failure mode, and is not present.
- **Two carve-outs, both deliberate.** A CROSS-TIER placement reference keeps the non-disclosing
  not-found through `resolvePlacementRef`: a 403 separating "no such location" from "a location you
  may not see" is the disclosure this whole model closes, one tier up
  ([ADR-0107](#adr-0107-a-component-creates-system-bind-is-a-system-write)). And the estate-wide
  resources (`principal`, `principal_group`, the `tag` registry, `node`) are all-or-nothing: they
  require an all scope and already answer 403, so there is no readable-but-not-actionable state for
  them to describe.
- **Proved per route, not per entity.** The authz conformance matrix's registry gains a per-entity
  list of the routes that hang off it, and drives all three branches through each. Per entity would
  not have done: the routes are not uniform across the registry (properties on all three tiers,
  membership and roles only on system, alarms only on component), so an entity-level assertion would
  have left twelve of the seventeen unproved.
- **Corrected on the way through.** The identity-access page explained the gap with "every scoped
  route resolves its target through the gateway with the action's own scope", which was already
  false when it was written: the tree CRUD split, and the matrix asserted it. The page now says the
  narrower true thing.
- **Not in scope, and filed:** `POST /components/{name}/commands:issue` fences its WRITE with
  `component:read` ([#749](https://github.com/hyperscaleav/omniglass/issues/749)), and the node
  placement bind's coverage gap stays open
  ([#750](https://github.com/hyperscaleav/omniglass/issues/750)). Neither is a status question, and
  folding either into an adoption sweep would have buried a security change inside it.
