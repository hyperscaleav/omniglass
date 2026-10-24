---
title: "Identity: a grant cannot exceed the granter"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: a grant cannot exceed the granter.** Grant creation is refused (403) when the granted role's
  capabilities are not covered by the granter's **all-scope** capabilities (`rbac.Set.Covers`, ADR-0013), so no
  caller can promote anyone (including itself) above its own tier: an **admin cannot grant `owner`** (`*:*`) and
  therefore cannot self-promote to superuser. `CreateGrant` previously checked only all-scope
  `principal_grant:create`, so admin could grant itself `owner@all` and log in as a superuser; the guard lives
  in the `create-grant` handler, mirroring the impersonation escalation guard, and only all-scope grants count
  (a narrowly-scoped capability cannot be conferred estate-wide). The deliberate stance: **admin is bounded on
  purpose**, the top management role, not the superuser; `owner` is the break-glass superuser and the
  owner-invariant anchor. Proven by a real-binary API test (admin cannot grant owner to a user or itself, 403;
  admin can grant viewer/operator it covers; owner can grant owner). Role editing will need the same rule.
