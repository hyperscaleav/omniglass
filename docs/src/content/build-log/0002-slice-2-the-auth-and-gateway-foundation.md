---
title: "Slice 2: the auth and gateway foundation"
date: 2026-06-27
refs: []
form: bullet
---

- **Slice 2: the auth and gateway foundation.** The [identity and access](/architecture/identity-access/)
  schema (principal with per-kind human and service, credential, role, principal_grant, and audit_log;
  uuidv7 keys), the four official roles seeded idempotently at boot, and `omniglass bootstrap <username>`,
  which creates the first owner (an `owner@all` grant plus a bearer credential) directly and idempotently.
  A bearer token resolves through the [Storage Gateway](/architecture/storage/) to its principal and
  grants, which flatten through the role index (inheritance, wildcards, the `:read` floor) into a
  capability set. Two seams every later route reuses: the **capability** fast-reject (401 unauthenticated,
  403 missing capability) and `GET /api/v1/auth/me` (principal, permissions, grants). `GET /api/v1/roles`
  is gated by `role:read`. Proven by testcontainer integration and end-to-end tests (bootstrap
  idempotency, the 401 and 403 paths, the auth/me contract) plus pure unit tests of the capability logic.
  Deliberate thin cuts this slice: bearer-token auth only, and scope resolves `owner@all` to all (the
  per-action `visible_set` resolver lands when there are entities to scope). Deferred: password and OIDC
  auth, the first-class agent principal, and CDC-driven cache invalidation of the role index.
