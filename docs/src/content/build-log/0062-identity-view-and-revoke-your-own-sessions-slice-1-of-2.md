---
title: "Identity: view and revoke your own sessions (slice 1 of 2)"
date: 2026-07-13
refs: []
form: bullet
---

- **Identity: view and revoke your own sessions (slice 1 of 2).** A signed-in user can now see and end
  their own sign-ins and API tokens. Two gateway primitives land on the existing `credential` table (no
  migration; `created_at` and `expires_at` already exist): `ListBearerCredentials` returns each of a
  principal's bearer credentials with only non-secret metadata (id, `ogp_` prefix, created, expiry) and
  never selects the raw `secret_hash` into a returned field (the request's own `sha256(token)` is compared
  in SQL to flag the `current` one, so the hash never leaves the database), and `RevokeBearerByID` deletes
  one **scoped to the owning principal** so a caller can only revoke their own. Over the API, `GET
  /auth/me/sessions` lists them (labelled `session` or `token` by the credential's `purpose`, the
  current one flagged) and `POST /auth/me/sessions/{id}:revoke` ends one (204); both are authn-only and
  self-scoped, so a credential id belonging to another principal is a non-disclosing 404, and revoking the
  current credential signs it out. The console's **Your profile** gains a **Sessions** card listing each
  credential with a **Revoke** action (**Sign out** on the current one), through the generated typed client
  and TanStack Query. The generated CLI gains `omniglass session list` / `session revoke <id>`. Proven by a
  storage test (list returns both with metadata and no secret and the current flag, revoke by id scoped to
  the principal, a cross-principal or malformed id is a no-op) and a real-binary API test (the current
  session is flagged, a second session is revoked and stops authenticating, another principal's id is a
  404, and revoking the current one signs out). The **admin** surface (an operator revoking another
  principal's sessions) is slice 2.
