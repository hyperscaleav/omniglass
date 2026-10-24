---
title: "Identity: force a password change after an admin reset"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: force a password change after an admin reset.** An admin reset now sets a new additive
  `human.must_change_password` flag; the user's own change-password clears it. While set, the `authn`
  choke point (the same place view-as read-only is enforced) refuses **every** route with a 403 except
  reading their own principal and the change itself, so the admin-known secret cannot be used to act.
  `GET /auth/me` carries `must_change_password`, and the console's `AuthGuard` swaps the whole app shell
  for a forced change-password screen until it clears (a `useChangePassword` that invalidates `/auth/me`
  releases the gate on success). Proven by a storage test (a reset sets the flag, a self-change clears
  it), a real-endpoint test (a flagged user reads `/auth/me` but every other route is a distinct
  "password change required" 403 until the change, then the gate releases), and a web test (the guard
  renders the forced screen only when flagged). Enforcement is a hard block on all routes; the CLI
  break-glass lanes do not set the flag.
