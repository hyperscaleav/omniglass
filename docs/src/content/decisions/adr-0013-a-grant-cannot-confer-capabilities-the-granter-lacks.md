---
title: "A grant cannot confer capabilities the granter lacks"
date: 2026-07-07
status: "Accepted"
summary: "Grant creation is refused when the granted role's capabilities exceed the granter's all-scope capabilities (admin cannot self-promote to owner)"
---

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Grant creation is refused (403) when the granted role's capabilities are not covered by the
  granter's **all-scope** capabilities (`rbac.Set.Covers`, the same primitive as the impersonation escalation
  guard). So no caller can promote anyone, including itself, to a tier above its own: an **admin cannot grant
  `owner`** (`*:*`), because admin is an enumerated role that does not cover the global wildcard. Issue
  [#109](https://github.com/hyperscaleav/omniglass/issues/109).
- **Context:** `CreateGrant` previously checked only that the granter held all-scope `principal_grant:create`
  (`action.All`), not that the granter covered the granted role, so an admin could grant itself `owner@all` and
  log in as a superuser, leaving the admin/owner distinction unenforced. The check lives in the `create-grant`
  handler (capability is a route/handler concern; ABAC scope stays the gateway's), mirroring the impersonation
  guard. Only the caller's **all-scope** grants count, so a capability held through a narrower grant cannot be
  conferred estate-wide (the same reason act-as requires all-scope cover). The consequence is a deliberate
  stance: **admin is bounded on purpose**, the top management role, never the superuser, and does not auto-gain
  future resources; `owner` (`*:*`) is the break-glass superuser and the [owner-invariant](#the-owner-invariant)
  anchor. The same cover rule must extend to role editing when it lands (you cannot edit a role above your own
  tier); tracked with that slice.
- **Refines:** [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover) (reuses its capability-cover primitive on the grant path).
- **Closes the gap:** issue [#109](https://github.com/hyperscaleav/omniglass/issues/109).
