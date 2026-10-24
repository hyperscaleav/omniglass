---
title: "Every credential is time-bounded; token `purpose`, not expiry shape"
date: 2026-07-11
status: "Accepted"
summary: "Every credential is time-bounded (reverses tokens-never-expire): session 12h, token / bootstrap 90d default with a `--ttl` capped at 365d; a `credential.purpose` column, not the expiry shape, tells session from token"
---

- **Date:** 2026-07-11 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** All credentials are time-bounded (reverses the earlier tokens-never-expire choice). A
  web-login session keeps a 12h absolute lifetime; CLI/API tokens and the bootstrap token get a 90-day
  default expiry with a `--ttl` override capped at 365 days; nothing is issued without an expiry. Sessions
  and API tokens are distinguished by a `credential.purpose` column, not by whether `expires_at` is set.
  Expiry is enforced lazily at authentication; there is no background sweep, and session/token lists show
  only live credentials. Deferred: a sliding idle timeout, a housekeeping sweep of long-expired rows, and
  nearing-expiry notifications.
- **Context:** The credential-expiry slice ([#157](https://github.com/hyperscaleav/omniglass/issues/157))
  bounded only the web-login session and left the CLI/API token unbounded (`expires_at` null), overloading
  "has an expiry" to mean "is a session". That left an eternal secret in the field, against the every-secret-
  rotates principle, and coupled the session-vs-token distinction to a nullable column that both kinds now
  populate. A dedicated `purpose` column names the concept directly, so the list and the console read the
  discriminator rather than inferring it, and the default 90-day / 365-day-cap window keeps a minted token
  usable for real automation without becoming permanent. `AuthenticateBearer` already refused a passed
  expiry, so enforcement needed no change: giving tokens a future expiry is enough, and the list reuses the
  same `expires_at is null or expires_at > now()` filter so a dead row is never shown.
- **Reverses:** the tokens-never-expire behavior introduced with
  [#157](https://github.com/hyperscaleav/omniglass/issues/157).
- **Closes:** issue [#172](https://github.com/hyperscaleav/omniglass/issues/172) (self-service sessions and
  the every-credential-expires model).
