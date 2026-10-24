---
title: "Identity: bearer session expiry"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: bearer session expiry.** Bearer credentials gain a nullable `credential.expires_at` (a new
  additive migration), and `AuthenticateBearer` refuses a credential whose expiry has passed (an expired
  session authenticates nothing). A web login installs its session cookie with a fixed absolute lifetime
  (12h), setting both the credential's `expires_at` and the cookie `Max-Age`, so a stolen session cookie is
  no longer valid forever; the `IssueBearerCredential` gateway method gained an `expiresAt` argument. CLI
  API tokens (`omniglass token`) and the bootstrap token pass a null expiry and do not expire, so
  automation keeps working. Proven by a storage test (a past-expiry bearer is `ErrCredentialNotFound`, a
  future or null one authenticates). A sliding idle timeout and a background sweep of expired rows are
  deferred refinements; the security floor (expired is refused at auth) lands here.
