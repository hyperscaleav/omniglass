---
title: "Identity: every credential is time-bounded, and sessions split from API tokens"
date: 2026-07-13
refs: []
form: bullet
---

- **Identity: every credential is time-bounded, and sessions split from API tokens.** Building on the slice
  above, the tokens-never-expire behavior is reversed ([ADR-0019](/architecture/decisions/#adr-0019-every-credential-is-time-bounded-token-purpose-not-expiry-shape)):
  a web-login **session** keeps its 12h absolute lifetime, while a CLI/API **token** (`omniglass token`) and
  the **bootstrap token** (`omniglass bootstrap`) now get a **90-day default** expiry with a `--ttl` override
  hard-capped at **365 days** (a `--ttl` above the cap is a clean error, before any DB work), so no eternal
  secret sits in the field. Because both kinds now carry an expiry, a new **`credential.purpose`** column
  (`session` / `token`), not the nullable `expires_at`, is the discriminator; a migration adds it and
  backfills existing bearers (expiry set to `session`, else `token`). `IssueBearerCredential` and
  `BootstrapOwner` take the purpose and expiry; `ListBearerCredentials` returns the purpose and now filters
  to **live** rows only (`expires_at is null or expires_at > now()`, mirroring `AuthenticateBearer`), so a
  dead credential is never listed. The console's **Your profile** splits the one list into a **Sessions**
  section and an **API tokens** section, both rendering a shared `SessionsList` primitive, and a token now
  shows its expiry. Enforcement stays **lazy** (an expired row is refused at auth, no background sweep).
  Proven by a storage test (the list carries the right purpose and excludes an expired row), an updated
  real-binary API test (session vs token asserted via purpose, the minted token carries a future expiry), and
  a CLI unit test (a `--ttl` above the 365-day cap errors for both `token` and `bootstrap`). Deferred: a
  sliding idle timeout, a housekeeping sweep of long-expired rows, and nearing-expiry notifications.
