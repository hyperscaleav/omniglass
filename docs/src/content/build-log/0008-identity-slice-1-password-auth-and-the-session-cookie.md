---
title: "Identity slice 1: password auth and the session cookie"
date: 2026-07-01
refs: []
form: bullet
---

- **Identity slice 1: password auth and the session cookie.** Humans now sign in to the console with a
  username and password, the local-password method the [identity model](/architecture/identity-access/)
  designs: a `password` credential (argon2id, PHC-encoded, one per principal). `POST /api/v1/auth/login`
  verifies it and sets an httpOnly, `SameSite` session cookie, so the console no longer holds a token
  (services and the CLI still send a bearer header); `POST /api/v1/auth/logout` revokes the session and
  clears the cookie, and
  the authn middleware accepts either. `omniglass bootstrap --password` sets the first owner's password.
  Proven by argon2 unit tests, a storage round-trip, a real-binary login integration test, and a browser
  e2e that signs in through the form. Thin cuts: self profile edit and change-password (slice 2), admin
  user CRUD and role assignment (slice 3); email invite and forgot-password are later.
