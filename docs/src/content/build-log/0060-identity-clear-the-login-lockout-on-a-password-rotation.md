---
title: "Identity: clear the login lockout on a password rotation"
date: 2026-07-10
refs: []
form: bullet
---

- **Identity: clear the login lockout on a password rotation.** Rotating a password now clears the
  brute-force lockout (`failed_login_count = 0`, `locked_until = null`) in the same transaction as the new
  secret. Before this the lock (from the login-throttle slice) only cleared lazily at the next login, so an
  admin reset left the account locked for the rest of the 15-minute window even with the new password. Both
  rotation lanes clear it: the admin reset (`SetPrincipalPassword`, the reachable intervention while an
  account is locked) and the self-service change / CLI set-password (`SetPassword`). No new endpoint or
  capability: rotating the secret is the intervention (auto-unlock over a dedicated `:unlock` action).
  Proven by a storage test that locks the account with five misses, confirms it is locked, rotates via each
  lane, and asserts the new secret authenticates immediately with no wait. No migration (the columns exist);
  no operator-facing surface change.
