---
title: "A membership or a role assignment costs a write on both of its ends"
date: 2026-10-26
status: Accepted
summary: "Every route that binds, unbinds, re-defaults or staffs a component in a system (`PUT` and `DELETE /systems/{name}/members/{component}`, `:setPrimary`, and `PUT` and `DELETE` on a role's assignment) needs `component:update` over the component as well as `system:update` over the system. The component end used to resolve by existence alone, so a principal scoped to one system could bind a component it could not even read, make that system the component's primary, and inject its own system-band variables and tags onto it through the cascade. The permission half is published as the routes' `x-omniglass-conditional-permission` and refused with a 403 naming it; the scope half runs in the Storage Gateway, where a bind resolves the component in `component:read` and requires `component:update` (a non-disclosing 404 out of read, a 403 out of update), and a write naming an existing member or occupant requires `component:update` alone (a 403, since the system's own read names it). System-wide acts that release bindings without naming a component (deleting the system, withdrawing a role, changing the standard) stay gated on the system alone"
refs: [510, 504]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/),
  [api](/architecture/api/), [core entities](/architecture/core-entities/)
- **Decision:** (#510, the architect's ruling in session for the 0.1.0 release, #825.) A
  membership or a role assignment writes onto **both** of its ends, so it costs a write on both.
  1. **The five routes that name a component** (add, remove and re-default a member; assign and
     unassign a role) need `component:update` beside the `system:update` that gates them. The
     handler checks the permission before anything resolves and refuses with a 403 naming it, as
     the component create does for the system it binds
     ([ADR-0107](#adr-0107-a-create-that-writes-a-membership-costs-what-the-membership-route-costs));
     the permission is published as the route's `x-omniglass-conditional-permission`, the condition
     being the component its path always names.
  2. **The component end is scoped in the gateway**, in sets resolved for the component tier. A bind
     (add, assign) resolves the component in `component:read` and requires `component:update`, the
     read-then-action split
     ([#736](https://github.com/hyperscaleav/omniglass/issues/736)) applied one tier over: out of
     read is the non-disclosing 404, readable but not writable a 403. Ambiguity is judged inside
     `component:read`, so a refusal lists only rows the caller may read. A write naming a component
     already related to the system (remove, re-default, unassign) resolves it among the members or
     occupants and requires `component:update` alone, a 403 either way, because the system's own
     member list and role read already name it and a 404 would deny a row the caller is looking at.
     A name that is not a member or an occupant here is told apart from one that names nothing only
     within `component:read`: a component outside it answers the same 404 a missing name does, so
     no removal route is an oracle for which components exist.
  3. **A system-wide act that settles bindings without naming a component** (deleting the system,
     withdrawing a role it declares, changing its standard) stays gated on the system alone. It acts
     only on components the system already holds: deleting the system can move a member's primary
     onto another system that already holds it, and a standard change carries an occupant onto the
     new standard's role of the same name
     ([ADR-0506](#adr-0506-changing-a-systems-standard-carries-its-staffing-onto-the-roles-that-still-exist)),
     but neither binds a component the system was not already bound to, so no system-scoped act
     reaches a component it did not already hold.
- **Context:** The component end resolved by existence alone (`scopedByName`), because the only
  scope in hand was resolved for `system` and a component's ancestor chain is unrelated to it.
  Reproduced live: a principal holding `system:create,update,delete` scoped to one system, with no
  component permission at all, bound a component it could not `GET` (204), staffed it into a role
  (204), and a variable it then created on its own system read back as the winning value on the
  victim's effective variables, because the first membership becomes the component's primary and
  the cascade seeds its system band from the primary
  ([ADR-0052](#adr-0052-the-cascade-resolves-through-membership-and-secrets-carry-no-system-band)).
  No ADR had blessed unfenced cross-tier writes, and [identity and access](/architecture/identity-access/)
  already named the cross-tier expansion as unbuilt rather than as a decision, so the reading was a
  known-unbuilt fence, exploitable at exactly the one write that reaches across tiers by design.
  The conformance matrix gives its narrow principals component authority over the rack its routes'
  components are made under, so it keeps driving the system's own split, and
  `internal/api/role_staffing_e2e_test.go` drives the component end's through all five routes.
  The accepted narrowing: a custom role carrying `system:update` and no `component:update` no
  longer binds anything; every seeded role that holds one holds both.
