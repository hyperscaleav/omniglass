---
title: "Principals are gated at all-scope, not scope-tree"
date: 2026-07-01
status: "Accepted"
summary: "A principal is not a scope-tree entity; the `principal` capability confers access only at all-scope"
---

- **Date:** 2026-07-01 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A `principal` is not a scope-tree entity: it is not "under" a location, system, or component,
  so the `principal:<action>` capability confers access **only at all-scope**. A grant scoped to a location
  or system carries no principal access, and the Storage Gateway refuses a non-all scope on the principal
  directory with a 403 (`ErrPrincipalForbidden`) rather than silently returning an empty list. This falls out
  of the scope resolver: `applicableKinds("principal")` is empty, so only an `all` grant resolves to a
  non-empty set.
- **Context:** The admin principal directory (slice 3a, [issue #77](https://github.com/hyperscaleav/omniglass/issues/77))
  is the first surface to gate on `principal:*`. Modelling users as scope-tree entities would be wrong (there
  is no "users under HQ"), and returning an empty list to a mis-scoped admin would hide a misconfiguration, so
  making all-scope explicit keeps the capability honest and surfaces the error. The same rule governs the later
  principal-mutation and grant surfaces.
- **Closes the gap:** n/a (a design decision, not a divergence).
