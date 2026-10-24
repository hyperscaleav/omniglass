---
title: "Identity slice 2: the self-service profile"
date: 2026-07-01
refs: []
form: bullet
---

- **Identity slice 2: the self-service profile.** A signed-in operator manages their own account, whatever
  their role: `PATCH /api/v1/auth/me` edits their own display name (email stays administrator-set), and
  `POST /api/v1/auth/me:changePassword` (the first AIP `:verb` custom method on the surface) verifies the
  current password and sets a new one, reusing the slice-1 argon2 primitive. Both are authn-only and
  self-scoped (they touch only the caller's own principal), so they join the small ungated allow-list
  alongside `GET /api/v1/auth/me` rather than carrying a
  capability. The console gains a **Your profile** page (profile form, change-password form, and a read-only
  Access panel that teaches the principal, grants, and permissions it operates on); the CLI gains
  `auth update-profile` and `auth change-password` from the generator. Proven by a storage round-trip, a
  real-binary integration test (wrong current password 403, short new 422, rotation, self-scope), and the
  client hook units. Thin cut: changing a password does not revoke existing sessions (a later hardening);
  admin user CRUD and role assignment are slice 3.
