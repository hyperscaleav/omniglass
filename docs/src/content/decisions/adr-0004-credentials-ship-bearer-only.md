---
title: "Credentials ship bearer-only"
date: 2026-06-27
status: "Resolved"
summary: "Bearer shipped first; `password` credentials (argon2id) landed in identity slices 1-2. OIDC / NATS still deferred"
---

- **Date:** 2026-06-27 | **Status:** Resolved (identity slices 1-2) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** Password credentials shipped in identity slice 1 ([#35](https://github.com/hyperscaleav/omniglass/pull/35)) and slice 2 ([#70](https://github.com/hyperscaleav/omniglass/pull/70)): `credential.kind` now allows `bearer` or `password` (argon2id, PHC-encoded, one password per principal), and a human signs in with a username and password behind an httpOnly session cookie. The `oidc` / `nats` methods and the full `(method, identifier)` lookup key remain deferred (future slices).
- **Decision (divergence):** The shipped `credential` table carries `kind = 'bearer'` only, stored as the
  token's sha256 with a non-secret `ogp_` locator prefix. The design's fuller model (the `password`,
  `oidc`, and `nats` methods, and the `(method, identifier)` lookup key) is **deferred**, not yet built.
- **Context:** The auth foundation slice needed exactly one working authN method to prove the capability and
  scope seams end to end. Bearer tokens are the thinnest honest cut: a service credential the bootstrap and
  the CLI can both carry. Password login is the first slice of the [identity tier epic (#27)](https://github.com/hyperscaleav/omniglass/issues/27)
  ([slice #28](https://github.com/hyperscaleav/omniglass/issues/28)), which adds `password` to the
  `credential.kind` CHECK in a new migration (never editing the applied one). OIDC and the NATS node
  credential follow with their own surfaces.
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).
