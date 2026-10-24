---
title: "Identity: permissions are topic patterns"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: permissions are topic patterns.** The capability matcher became a consistent NATS-style topic
  match (ADR-0015, superseding the ADR-0014 carve-out): `*` matches exactly one token and `>` matches the tail,
  so a two-token pattern (`*:read`, `*:*`, `principal:*`) structurally cannot reach a three-token `:admin`
  permission, and admin-sensitivity is a deeper token rather than a special case. `audit:read` became the
  admin-sensitive `audit:read:admin` (the audit route requires it), `owner` became `>`, and the
  `sensitiveResources` set and `grantsAll` helper were deleted. `Set.Allows` matches by token (with the
  unchanged `:read` floor); `Set.Covers` (the impersonation and grant-escalation guard) became pattern
  subsumption plus the floor, staying conservative. The one seed change is `owner`'s `*:*` -> `>`; every other
  permission keeps its meaning (`*` already meant a single token). As a free consequence, `principal:*` no
  longer sweeps a future admin-tier `principal:<action>:admin`. Proven by an rbac unit matrix (`*` one token,
  `>` tail, `*:read`/`*:*`/`principal:*` miss `:admin`, `>` and explicit hit it, Covers subsumption), the whole
  authz suite (the viewer is still 403 on the audit trail, now structurally), and web tests (the Roles view
  renders `>` as an "everything" chip and marks an `:admin` tier). The console Roles view and grant-builder
  render the new grammar. Foundational for a custom-role permission preview (a permission catalog).
