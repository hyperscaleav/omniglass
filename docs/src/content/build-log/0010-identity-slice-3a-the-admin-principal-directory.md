---
title: "Identity slice 3a: the admin principal directory"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity slice 3a: the admin principal directory.** The first surface to exercise the already-seeded
  `principal:*` capability: `GET /api/v1/principals` (with a `kind` filter) and `GET /api/v1/principals/{id}`
  list and read every principal with its grants, and `POST /api/v1/principals` creates a human with an
  optional initial password (argon2id, reusing the slice-1 credential; no new migration). All three are gated
  by a `principal` capability that resolves **only at all-scope**: a principal is not a scope-tree entity, so
  a location or system grant confers nothing and the gateway answers a non-all scope with 403, proven both
  ways. Responses carry credential-free profiles and grants, so no secret ever leaves the API. The console
  `/users` stub becomes a real **Users** directory (grid, detail panel, and a create form); the CLI gains
  `principal list` / `get` / `create`. Proven by a storage round-trip, a real-binary integration test
  (all-scope allow, scoped-deny 403, duplicate 409, short-password 422, secret redaction, created-human
  login), the route-gating guard, and the client data-layer units. Thin cuts: no `created_at` column in the
  directory yet, and create is human-only. Deferred: update / disable / delete a principal, role assignment,
  the owner-invariant trigger, and service-account and credential management.
