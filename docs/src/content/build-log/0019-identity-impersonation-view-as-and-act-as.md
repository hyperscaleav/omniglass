---
title: "Identity: impersonation (view-as and act-as)"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity: impersonation (view-as and act-as).** An owner or all-scope admin holding
  `principal:impersonate` can `POST /principals/{id}:impersonate` to mint a bounded, revocable token to
  **view as** (read-only) or **act as** (full) another principal, and `POST /auth/me:stopImpersonation` to
  end it. Two guarantees: the **escalation guard** (`rbac.Set.Covers`) refuses impersonating a principal
  whose capabilities exceed the caller's, so no capability is ever gained; and **dual-actor audit**, a new
  nullable `audit_log.real_actor_principal_id` (threaded via a request-scoped context value, no gateway
  signature churn) records the true admin behind every impersonated mutation. The token is an
  `impersonation_session` (its own table, not a credential); `authn` resolves it on a bearer miss to the
  target, and `require()` refuses every non-read action in view-as. Self and nesting are refused; disabling
  either party kills the session on its next request (ADR-0008 / ADR-0010). Proven by a storage round-trip
  and a real-binary API test (act-as writes in the target scope with a dual-actor audit row, view-as GET 200
  / PATCH 403, a lesser admin cannot impersonate the owner, self 422, a viewer capability-gated, stop kills
  the token, a split-grant admin cannot act-as into a disjoint scope). The console ships an **Impersonate**
  action (View as / Act as) on the user card and a persistent **acting-as banner** with Stop. Deferred:
  act-as within a scoped admin's own scope by intersection (#101; act-as currently requires all-scope over
  the target's write capabilities) and re-checking the guard per request (bounded by the TTL + revoke for now).
