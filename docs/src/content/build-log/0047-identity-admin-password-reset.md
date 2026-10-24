---
title: "Identity: admin password reset"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: admin password reset.** An administrator can reset another user's password without the
  user's current one: `POST /principals/{id}:resetPassword` gated by a new `principal:reset-password`
  capability (all-scope; admin holds it via `principal:*`, so a future help-desk role can be granted only
  the reset), policy-enforced (a 422 on a weak password), refused on self (change your own password from
  your profile, which verifies the current one), behind the same **takeover guard** as
  impersonation (an owner cannot be reset by anyone, and a caller cannot reset a principal whose
  capabilities exceed their own, a shared `checkTakeoverGuard`, plus the shared `allScopeCovers`
  all-scope-only cover that act-as uses so a reset cannot promote a narrow-scope capability estate-wide),
  and audited with the admin as the actor (the
  storage method `SetPrincipalPassword` sets by id, distinct from the self-service `SetPassword` that keys
  on username and audits the target). It also **forces logout**: a reset revokes every one of the target's
  bearer credentials (all sessions and tokens) in the same transaction, so a compromised or departing
  account is cut off at once; the self-service change-password revokes the caller's OTHER sessions
  (`RevokePrincipalBearers` keeping the current session's hash, stashed in the request context by authn). The console adds a **Reset password** kebab action on the user blade
  that opens an inline panel reusing the `PasswordField` (Generate, Copy, inline policy check); the set
  password stays copyable to hand over. This is the console counterpart to the CLI `set-password` (which,
  as a trusted direct-DB lane, stays policy-exempt). Proven by a storage test (the new secret
  authenticates and the old does not, the reset is audited as the admin, non-all scope and unknown id are
  refused), an API test (an operator lacks the capability 403, a weak / common / username-containing
  password is a 422, resetting yourself is a 422 and an owner is a 403, the admin reset lets the user
  sign in with the new password), and web tests (the kebab opens the panel and Generate + Set password
  calls the endpoint and confirms; the action is hidden on your own blade).
