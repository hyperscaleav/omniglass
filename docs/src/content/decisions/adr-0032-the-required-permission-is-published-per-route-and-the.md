---
title: "the required permission is published per route, and the permission universe is route-derived"
date: 2026-07-17
status: "Accepted"
summary: "Every gated route stamps its required permission into the OpenAPI (`x-omniglass-permission`), so the permission universe is derived from the routes rather than a hand-kept catalog"
---

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [API](/architecture/api/), [Access guide](/guides/admin/access/)
- **Decision:** Every capability-gated route registers through one helper, `gated(op, tokens...)`,
  which sets the `authn` + `require` middleware (unchanged enforcement), stamps the operation with
  an `x-omniglass-permission` OpenAPI extension, and records the permission in an in-process
  registry. The required permission for each request is therefore **published in the generated
  `api/openapi.json`**, and the **permission universe** (the deduped set of every stamp) is
  **derived from the routes**, not a hand-kept catalog. `GET /roles` reports the universe plus, per
  role, the **held** subset (resolved by the same `rbac.Set.Allows` matcher as the effective set),
  and the console role blade renders it as a net `Held / Missing / All` view. Two build-time guards
  keep it honest: a **published-gate guard** (every gated route is stamped, allow-listed routes are
  not, so "gated" and "published" are the same set) and a **seed-drift guard** (every seed-role
  grant resolves into the universe or sits in an explicit `aheadOfRoutes` allow-list).
- **Context:** the authz contract already existed (`require(...)` enforced a permission on every
  route) but lived only in Go middleware, invisible to the spec, the clients, and any reader; and
  a role blade could show only what a role granted, never the capabilities it lacked. Three options
  were weighed for the universe source: a hand-kept catalog YAML (drifts), a runtime-only set
  (invisible in diffs), and the route-derived stamp (self-maintaining, reviewable in the committed
  spec, exactly the enforced surface). The stamp won because it makes the universe fall out of the
  API-first pipeline with no second source to drift. Held is resolved server-side so the single
  rbac matcher is not duplicated in the SPA. Grants that resolve to nothing (for example
  `alarm:*`, `interface:*` before those subsystems have HTTP surfaces) are legitimate but ahead of
  their routes; they show as held-nothing and are allow-listed until the route lands.
- **Lands:** issue [#272](https://github.com/hyperscaleav/omniglass/issues/272) under epic
  [#27](https://github.com/hyperscaleav/omniglass/issues/27). Design:
  `docs/superpowers/specs/2026-07-17-net-permissions-role-blade-design.md`.
