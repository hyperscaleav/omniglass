---
title: "A sign-in's PKCE verifier and nonce are derived from its browser binding, not stored"
date: 2026-11-23
status: "Accepted"
summary: "An OIDC sign-in stores only the hash of its state and the hash of its browser binding (the value in the short-lived og-oidc cookie). The PKCE verifier and the nonce are HMACs of the binding, recomputed at the callback from the cookie, so neither is stored at all. Refines ADR-2361 point 3's 'stored hashed as oauth_code is'."
refs: [2362, 2361]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#credentials), [single sign-on](/deployment/production/#single-sign-on)
- **Decision:** (#2362, refining [ADR-2361](#adr-2361-single-sign-on-links-a-person-to-an-identity-provider-subject-never-by-email)
  point 3, which said the state and nonce are stored hashed with a short expiry as `oauth_code` is.)
  The start route mints two random values: the **state**, sent to the provider, and the **binding**,
  set in the httpOnly `SameSite=Lax` `og-oidc` cookie scoped to `/auth/oidc`. The `oidc_state` row
  holds only the hash of each, the provider's name, the self-link's user and session hash, and a
  ten-minute expiry, and is spent once. The PKCE verifier and the nonce are each an HMAC-SHA256 of the
  binding (`sso.Derive`), so the callback recomputes them from the cookie and the database holds
  neither, hashed or not.
- **Context:** A verifier cannot be stored hashed: the token exchange sends it in the clear, so a
  stored form would have to be the verifier itself, sealed. Deriving it from the binding the login
  CSRF check already needs keeps every stored value a hash, and ties the code exchange to the browser
  that started the flow twice over: without the cookie, the callback refuses the state, and could not
  produce the verifier anyway.
- **Not chosen:** sealing the verifier and nonce into the row (a stored secret for no gain), or
  deriving them from the state (the state travels in the redirect URL, so anyone who saw the URL could
  recompute the verifier, which is what PKCE exists to stop).
