---
title: "A person signs in with OIDC (#2362)"
date: 2026-11-23
refs: [2362, 2361, 527]
---

The first single sign-on slice of ADR-2361. With the `sso` settings naming an issuer and an
`oauth2-client` platform secret, the sign-in page offers **Sign in with SSO**:
`/auth/oidc/start/{provider}` sends the browser to the provider with PKCE (S256), a nonce and a state
bound to the browser by a short-lived cookie, and `/auth/oidc/callback` spends the state once,
exchanges the code, verifies the id_token's signature, issuer, audience, expiry and nonce, and signs
the linked person in with the ordinary session, auditing the `login` with method `oidc`. An unlinked
identity is refused with a message naming it. The migration adds `oidc` to `credential.kind`, an
`identifier` unique per kind, a nullable `secret_hash` under a per-kind check, the link's sealed
refresh token and grace, `via_link` on the credentials minted through a link, and `oidc_state`.

Admins link a person under the new `user:link-identity` behind the takeover guard; a person links
their own with their password through a flow bound to their session; an owner grant deletes the links
someone else made. The re-check sweep redeems each live link's refresh token every
`recheck_minutes`: a refusal revokes every session and API token through the link in one audited
transaction, an outage is retried and refused at authentication only past `recheck_grace_minutes`, and
a provider that issues no refresh token gives a one-hour session that cannot mint API tokens.

New primitives: `egress.Client.HTTPClient` (a library's calls under the egress contract),
`auth.JWTVerifier` (the JWKS-backed verifier #81 reuses) and `internal/auth/oidctest`, a fake
provider for tests. Proven end to end against the fake and against a real Dex in a testcontainer.
The verifier and nonce are derived from the browser binding rather than stored (ADR-2362).
