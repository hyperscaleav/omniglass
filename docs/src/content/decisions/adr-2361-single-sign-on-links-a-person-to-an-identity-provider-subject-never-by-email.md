---
title: "Single sign-on links a person to an identity provider subject, never by email"
date: 2026-11-23
status: "Accepted"
summary: "A person signs in through one OIDC identity provider (authorization code with PKCE) by an `oidc` credential whose identifier is `iss|sub`, linked by an admin or by the person; SAML 2.0 follows on the same model. Email never links an account. IdP groups sync IdP-sourced memberships at each sign-in and may not reach an owner grant. Removal at the provider ends every SSO-origin session and API token within one re-check interval (15 minutes), by refreshing each link against the provider; no SCIM. One provider per install, shaped so a second is additive. Owners keep password sign-in as break-glass; an enforce mode refuses it for everyone else. Reverses ADR-0004's OIDC deferral"
refs: [2361, 527, 2362, 2363, 2364, 2365, 81]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#credentials)
- **Decision:** (#2361, reversing the `oidc` deferral in
  [ADR-0004](#adr-0004-credentials-ship-bearer-only); the slices that build it are
  [#2362](https://github.com/hyperscaleav/omniglass/issues/2362) (OIDC sign-in for linked persons),
  [#2363](https://github.com/hyperscaleav/omniglass/issues/2363) (group mapping),
  [#2364](https://github.com/hyperscaleav/omniglass/issues/2364) (provisioning and enforce mode) and
  [#2365](https://github.com/hyperscaleav/omniglass/issues/2365) (SAML 2.0).)
  1. **An identity provider identity is a credential.** A new migration (the init migration is
     never edited) adds `oidc` to the `credential.kind` CHECK (#2365 adds `saml` the same way), a
     nullable `identifier` text column, and makes `secret_hash` nullable, with a CHECK that a
     `bearer` or `password` row still carries its hash and an `oidc` row carries an identifier and
     no hash. A unique index on `(kind, identifier)` where the identifier is set makes one identity
     one person. The identifier is `iss|sub`: the id_token's `iss` exactly as the provider sends it
     and its `sub` verbatim, never normalized, since `sub` is unique only within its issuer. The row
     is a link, not a session: it holds no Omniglass secret, so it carries no expiry ([ADR-0019](#adr-0019-every-credential-is-time-bounded-token-purpose-not-expiry-shape)'s time bound is
     on the credentials that are bearer secrets) and no `purpose`. It does hold the provider's latest
     refresh token, sealed with the install's encryption key as every stored secret is, and the time
     the provider last accepted it (point 9). A sign-in through it mints the ordinary 12-hour
     `session` credential, through the path password sign-in uses, and writes the `login` auth event
     with the method. That session, and every API token minted while signed in through it, records
     the link it came through (a nullable `via_link` reference on `credential`), which is what point 9
     revokes. A person may hold more than one link.
  2. **Linking is explicit; email never links.** In the first slice an unlinked identity is
     refused with a message naming who can link it. An admin links a person to an `iss|sub` under
     a new `user:link-identity` permission (all scope), behind the takeover guard that
     `user:reset-password` uses, because a link an admin chooses is a way to sign in as its target:
     so no one links another owner. A person links their own identity with their current password,
     as a password change asks, since a link outlives the session that made it; that is how an
     owner gains SSO. Unlinking takes the same gate. Every link and unlink is audited, and a link
     records who made it: the guard judges the target only at link time, so when a person is granted
     owner every link someone else made for them is deleted (audited), and they link again themselves.
     Just-in-time provisioning (#2364) is a separate opt-in that only ever creates a new person; when
     the claims' email matches an existing person it refuses and asks for a link instead of adopting
     the account. A new person's email is taken from the claims only when `email_verified` is true.
  3. **OIDC first, SAML on the same model.** OIDC is the authorization code flow with PKCE (S256),
     a `state` and a `nonce`, stored hashed with a short expiry as `oauth_code` is, as a confidential
     client. The state is also bound to the browser that started the flow: start sets it in a
     short-lived httpOnly `SameSite=Lax` cookie and the callback refuses a state its cookie does not
     carry, so a callback URL an attacker completed as themselves cannot sign a victim into the
     attacker's account (login CSRF). A self-link flow is bound to the signed-in session the same way. The id_token is verified for signature against the provider's JWKS, `iss` equal to the
     configured issuer, `aud` containing the client id (and `azp` when there are several), `exp`
     and `iat` within a small skew, and the nonce; claims come from the id_token alone, so the groups
     claim must be in it. Sign-in starts at Omniglass (no provider-initiated sign-in). The callback is
     the request's origin, under the trusted-proxy rule the OAuth server's issuer uses, plus
     `/auth/oidc/callback`, registered at the provider, which checks it. Sign-in starts at
     `/auth/oidc/start/{provider}`, and the stored state names the provider, so one callback serves
     every provider (point 10). The default scopes add `offline_access`, so the provider issues the
     refresh token point 9 re-checks. Verification uses
     `github.com/coreos/go-oidc/v3` over `golang.org/x/oauth2`. SAML 2.0 (#2365) follows, SP-initiated,
     as kind `saml` with the identifier `<IdP entityID>|<persistent NameID or configured attribute>`,
     reusing the link, provisioning, enforce and group mapping below unchanged.
  4. **IdP groups map to Omniglass groups and may not reach an owner.** An admin maps a value of
     the groups claim, keyed by issuer, to a `user_group`. `user_group_member` gains a `source`
     (`local` or `idp`) and, on an `idp` row, the issuer that granted it, and at each SSO sign-in,
     inside the sign-in transaction, the person's `idp` memberships from that issuer are set to
     exactly the mapped set, each add and remove audited (another issuer's rows are not touched);
     `local` rows are never touched, and a `local` row for a group stands in for an `idp` one. A
     token with no groups claim maps to the empty set (fail closed). Scoped access then follows
     with no authorization change, since a member inherits group grants in the per-request grant load
     (`loadUser`). A mapping to a group that carries an owner grant is refused, and so is an owner
     grant on a group that has a mapping, so no group mapping can make an owner.
  5. **Owners keep password sign-in.** Local password sign-in stays for owners, and
     `omniglass set-password` stays the last resort for a locked-out owner. An enforce mode, off by
     default, refuses password sign-in for every person who is not an owner, with a message pointing
     at SSO; it removes no password and does not touch bearer tokens, agent tokens, or the
     direct-database lanes (`bootstrap`, `set-password`).
  6. **One `sso` settings namespace, the secret by name.** A platform-domain `sso` namespace holds
     the one provider, named `default`: `issuer` (empty turns SSO off), `client_secret` (the name of a `$platform` secret of the seeded
     `oauth2-client` type, settable from the environment per
     [ADR-2223.2](#adr-22232-a-platform-secret-may-be-declared-by-the-servers-environment)),
     `scopes` (default `openid email profile offline_access`), `groups_claim` (default `groups`),
     point 9's `recheck_minutes` (default 15) and `recheck_grace_minutes` (default 60), and #2364's
     `provision` and `enforce`, each settable from the environment layer of
     [ADR-2223](#adr-2223-settings-read-an-env-layer-between-the-settings-file-and-the-console). The
     client id is the secret's `client_id` field, not a second setting, so the pair rotates together.
     Naming the secret from the console or API takes `secret:bind` on it, as omnibot's `key_secret`
     does, and so does moving `issuer` while a secret is bound, as moving omnibot's `base_url` does:
     the token exchange sends the client secret to the issuer's token endpoint, so an issuer someone
     else controls would receive it.
  7. **Every provider call goes through egress, under the install's allow list.** Discovery, JWKS
     and the token exchange use `internal/egress`, so TLS is verified, redirects are not followed and
     each call is bounded. Unlike a client metadata document
     ([ADR-2074](#adr-2074-an-mcp-client-may-name-itself-by-a-metadata-document-url-fetched-through-egress)),
     whose URL an anonymous caller names, the issuer is configuration an admin set, so the install's
     `notification.egress_allow` list applies: an on-premises provider is reached only when it names it.
  8. **One token verifier for SSO and Cloudflare Access.** The JWKS-backed JWT verifier (a key set
     fetched through egress, cached by `kid`, refreshed on an unknown one, with the issuer and
     audience checks of point 3) is one primitive under `internal/auth`, built by #2362, and the
     Cloudflare Access trust of [#81](https://github.com/hyperscaleav/omniglass/issues/81) verifies
     its `Cf-Access-Jwt-Assertion` with it rather than a verifier of its own. An Access identity is
     an `iss|sub` like any other and resolves its person through the same link. #81's preview-only
     policy of provisioning every Access user as an owner is the one exception to point 4's spirit
     (no identity provider makes an owner): #81 records it, confines it to preview installs, and may
     not match a person by email (point 2).
  9. **Removal at the provider ends access within one re-check, without SCIM.** A sweep every
     `recheck_minutes` (default 15) takes each link that has a live credential through it (point 1's
     `via_link`) and redeems its refresh token at the provider's token endpoint, storing the rotated
     token and the time. The major providers refuse a refresh once a person is disabled, deleted or
     has their sessions revoked there (`invalid_grant`), and a refusal revokes, in one audited
     transaction, every session and API token through that link and clears its refresh token: the
     person can sign in again only if the provider lets them. When the refresh answers with an
     id_token that carries the groups claim, point 4's sync runs on it, so a group removed at the
     provider also lands within the interval. A network error or a 5xx is not a refusal: the sweep
     retries, but a credential whose link has gone `recheck_grace_minutes` (default 60) without an
     accepted refresh is refused at authentication until one is accepted, so a provider outage
     suspends SSO-origin access for a while and destroys nothing, and owners keep password sign-in
     (point 5). A provider that issues no refresh token gets no long-lived access: a session through
     such a link lasts `recheck_grace_minutes`, not 12 hours, and minting an API token from it is
     refused, since nothing would ever re-check it. #2362 builds this with the first sign-in, so SSO
     never ships without the cut-off.
  10. **One provider now, a second is additive.** The model already allows several: the issuer is in
      every link's identifier, in every group mapping's key and on every `idp` membership, and the
      state names the provider. Adding providers later is a `providers` map in the `sso` namespace,
      keyed by name, beside which the flat keys of point 6 go on meaning the provider named
      `default`; the sign-in page lists one button per provider; and the start route already takes the
      name. No existing link, mapping or membership is rewritten, and the callback registered at the
      provider does not change.
- **Context:** ADR-0004 shipped bearer and password credentials and deferred `oidc`. Installs that
  run an identity provider want their people to sign in with it, their leavers cut off there, and
  their building teams granted by the groups they already keep. The design already sketched the
  `oidc` credential as `iss|sub`; four slices needed one model to build against, and the account
  takeover paths (email matching, an IdP group reaching an owner, an admin choosing another person's
  link) needed closing before any code.
- **Limits:**
  - One identity provider per install (point 10). Changing `issuer` leaves every existing link on
    the old issuer, so those people cannot sign in by SSO until relinked.
  - Removal at the provider takes effect within one re-check interval plus however long the provider
    takes to refuse the refresh, not instantly; disabling the person in Omniglass is still the
    instant cut-off. The cut-off is only as good as the provider's refusal: a provider that keeps
    honoring a disabled person's refresh token defeats it, and the provider's own setup page says so
    for each provider the docs cover.
  - An API token minted by a password session (an owner's) is not an SSO-origin credential and is
    not re-checked, since its person never went through the provider.
  - Only groups claim values: no nested-group expansion, no userinfo call, and no handling of a
    provider that leaves an oversized groups list out of the token (Entra's group overage).
  - No SCIM, so a person removed at the provider stays an Omniglass person (unable to sign in by
    SSO once the provider refuses them) until an admin disables them.
  - MFA is the provider's; the open question on a local TOTP path for installs without SSO stands.
- **Not chosen:**
  - **Linking by verified email.** Convenient, but `email_verified` is only as good as the
    provider's control over the address: a provider that lets people edit their email, a reassigned
    address, or a second issuer asserting the same one would hand over an existing account, an
    owner's included.
  - **Provisioning on by default.** Every person at the provider would become an Omniglass person
    with whatever a mapped group gives; it stays a deliberate opt-in.
  - **An IdP group reaching an owner.** The provider's admins would become Omniglass owners and the
    owner floor would move outside the product; owner stays a grant an owner makes.
  - **Enforce mode for owners too.** A provider outage or misconfiguration would lock every owner
    out with only the database lane left.
  - **Several identity providers at once, now.** Point 10 shapes everything for it; the settings
    map, the sign-in page's list and the second issuer's mappings wait until an install needs one.
  - **A shorter SSO session as the removal cut-off.** An hour-long session bounds a browser but not
    an API token the person minted, which the provider never sees; and a session short enough to
    matter makes people sign in through the provider several times a day.
  - **Capping SSO-minted API tokens instead.** A cap short enough to bound a leaver (hours) makes the
    tokens useless to the scripts they exist for; a cap long enough to be useful (days) is the gap point 9
    closes.
  - **Checking userinfo with the access token.** Providers commonly let an issued access token run
    to its expiry (about an hour) after a person is disabled; the refresh grant is the one they
    revoke at once.
  - **Back-channel logout.** It tells Omniglass a session ended, not that a person was removed, and
    the providers that matter here do not all send it; it can be added later as an accelerator on
    top of the sweep.
  - **The implicit or hybrid flow, or a public client.** OAuth 2.1 drops the implicit flow, and the
    server can hold a client secret.
  - **A hand-rolled JOSE verifier.** go-oidc is the maintained verifier the ecosystem relies on;
    signature and claim checks are not where a slice should write its own.
