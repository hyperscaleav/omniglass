---
title: "Identity slice 3c: role assignment and the owner invariant"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity slice 3c: role assignment and the owner invariant.** `POST /api/v1/principals/{id}/grants`
  assigns a role at a scope and `DELETE /api/v1/principals/{id}/grants/{grantId}` revokes one, gated by
  `principal_grant:create` / `:delete` (all-scope). This is how a fresh user gets permissions. The
  **owner-invariant trigger** (ADR-0006, now resolved) lands here: a `DEFERRABLE INITIALLY DEFERRED` Postgres
  constraint trigger refuses to leave zero `owner @ all` grants at `COMMIT`, so revoking the last owner is a
  409 while a one-transaction owner swap still passes (mapped from the custom SQLSTATE `OG001` to
  `ErrLastOwner`). Bad inputs are caught: duplicate 409, unknown role 422, a scoped grant with no id 422,
  unknown grant 404. The console detail panel gains a grant chip **x** (revoke) and an **add-grant** form
  (role picker, scope kind, scope id); the CLI gains `grant create` / `grant delete`. Proven by a migration
  round-trip, a storage test (grant, revoke, the last-owner refusal, the swap), a real-binary integration
  test, and the client units. Deferred: disable / delete a principal, group grants, and editing a grant in
  place (revoke and re-grant instead).
