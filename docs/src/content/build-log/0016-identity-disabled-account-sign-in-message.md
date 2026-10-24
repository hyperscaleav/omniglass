---
title: "Identity: disabled-account sign-in message"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity: disabled-account sign-in message.** A disabled account that signs in with the **correct**
  password now gets an explicit "This account is disabled. Contact your administrator." (a distinct
  `403`), instead of the generic invalid-credentials message, so a locked-out operator knows to ask an
  admin rather than assuming a wrong password. The disclosure is **oracle-safe by construction**:
  `AuthenticatePassword` drops the `and pr.active` filter, always runs the argon2 verify (against the
  real hash, or a dummy hash of matching parameters on a miss, so there is no early-return timing leak),
  and only branches to the new `ErrAccountDisabled` **after** the password verifies. So a wrong password
  (or an unknown user) against a disabled account is indistinguishable from any other bad credential:
  same generic `401`, same work. The bearer/token path is unchanged. Proven by storage tests (correct
  password on a disabled account -> `ErrAccountDisabled`; wrong password -> `ErrBadCredentials`), a
  real-binary api test (disabled + correct -> `403`, disabled + wrong -> `401`), and a web unit. Deferred:
  **auditing** the disabled-login attempt (#97), which belongs at the api/outbox layer, not the gateway
  read, to avoid a write-on-read and an attacker-append vector.
