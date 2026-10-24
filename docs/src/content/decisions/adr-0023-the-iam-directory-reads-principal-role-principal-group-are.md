---
title: "the IAM directory reads (principal, role, principal_group) are admin-tier"
date: 2026-07-13
status: "Accepted"
summary: "The IAM directory reads (principal, role, principal_group) move to the admin tier (`<resource>:read:admin`), so viewer's `*:read` floor no longer reaches Users, Roles, and Groups"
---

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** The **read** (list and get) of `principal`, `role`, and `principal_group` moves from a two-token
  `<resource>:read` to the admin-sensitive **`<resource>:read:admin`**, so the `viewer` read floor (`*:read`) no
  longer reaches the Users, Roles, and Groups directories. `admin` carries an explicit `principal:read:admin`,
  `role:read:admin`, and `principal_group:read:admin` alongside its `<resource>:*` wildcards, the same shape as the
  existing `principal:purge:admin`; `owner`'s `>` is unaffected. Create, update, and the lifecycle verbs stay
  two-token: they were never reachable by a non-admin, so only the directory read needed promoting. The console
  gates the three Settings tabs on the same three-token permission and the route guard reads it from the shared nav
  map, so the sidebar and the server never diverge.
- **Context:** `deploy` (an integrator or field tech) inherits `viewer`, whose `*:read` is a single-token resource
  wildcard. Because `*` matches exactly one token, `*:read` matched `principal:read`/`role:read`/`principal_group:read`,
  and the read floor shares that reach, so a field tech could enumerate every user, role, and group over the API (a
  real 200, not just a visible menu). Promoting the directory reads reuses
  [ADR-0015](/architecture/decisions/#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)'s
  deeper-token rule rather than adding a matcher special case: admin-sensitivity is a third token `*` cannot reach.
  Secrets are a separate concern (an operator legitimately reads device secrets in scope), handled by a forthcoming
  slice that combines placement scope with a per-secret admin-sensitive flag; this ADR is the IAM directories only.
- **Closes:** issue [#197](https://github.com/hyperscaleav/omniglass/issues/197).
