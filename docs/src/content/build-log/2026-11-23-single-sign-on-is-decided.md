---
title: "Single sign-on is decided before it is built (#2361)"
date: 2026-11-23
refs: [2361, 527]
---

ADR-2361 settles how a person signs in through an identity provider, reversing ADR-0004's `oidc`
deferral: an `oidc` credential keyed `iss|sub` (then `saml`) that is a link, not a session; linking
by an admin under the takeover guard or by the person, never by email; OIDC with PKCE first, through
`internal/egress` under the install's allow list; IdP group claims syncing only IdP-sourced
memberships and never reaching an owner grant; removal at the provider ending every SSO-origin session and API token at the first 15-minute re-check the provider refuses, not instantly, with no SCIM; one provider per install, shaped so a second is additive; owners keeping password sign-in as break-glass; one
`sso` settings namespace with the client credentials in an `oauth2-client` platform secret; and one
JWT verifier shared with the Cloudflare Access trust of #81. ADR-0019 notes that a link carries no
expiry. The identity and access page points at the decision; #2362 to #2365 build it. No code.
