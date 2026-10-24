---
title: "Identity slice 3d: disable and enable a principal"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity slice 3d: disable and enable a principal.** `POST /api/v1/principals/{id}:disable` and
  `:enable` (AIP custom methods, gated `principal:update`, all-scope) soft-disable an account: a new
  `active` flag on `principal`, and `AuthenticateBearer` / `AuthenticatePassword` refuse a disabled
  principal, so it can neither sign in nor use a token. Its rows (and the audit trail that references it)
  are kept, which is why the model is disable, not delete: `audit_log` references the principal, so an
  actor that has acted cannot be removed. A **last-active-owner guard** refuses to disable the final active
  `owner @ all` (409), mirroring the grant trigger for the active flag. The console shows an **inactive**
  badge and a **Disable / Enable** toggle; the CLI gains `principal disable` / `enable`. Proven by a
  migration round-trip, a storage test (both auth paths refused, enable restores, last-owner refusal, the
  swap), and a real-binary integration test. Deferred: hard delete (would need an audit-actor
  `on delete set null`, a separate decision) and bulk operations.
