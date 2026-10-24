---
title: "Identity: the auth event log"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: the auth event log.** The `audit_log` already recorded every privileged mutation (with `actor`
  and, for impersonated actions, `real_actor`); this adds the read surface and captures auth events. `GET
  /audit-log` (newest first, filter by resource / verb, backward-paged by `before`) resolves each actor and
  real-actor to a username, and **login / logout are now captured** (`resource = 'auth'`, via a non-transactional
  emit seam, since login is a read/no-tx path), and so are **failed sign-ins**: a wrong password on a real
  account is `login_failed` (attributed to that principal) and a correct password against a disabled account is
  `login_denied` (#97), while an attempt on an unknown username is not written (so scanning cannot flood the
  log). The console gains a read-only **Audit** page (`Settings > Audit`): every action and sign-in, with an
  `as <actor>` tag on anything done while impersonating. It is **admin/owner-only**: `audit` is a sensitive
  resource, so a `viewer`'s `*:read` does **not** confer `audit:read`, only an explicit grant or `*:*` (a small
  `rbac` sensitive-resource carve-out, ADR-0014). Proven by an rbac unit test (the carve-out: `*:read` denied,
  `*:*` and explicit allowed), a storage round-trip, a real-binary API test (a login appears, a real-account
  failed login is attributed while an unknown-user attempt is not, a viewer is 403, the resource filter
  narrows, and an impersonated action carries `real_actor`), and web tests (the Audit page renders and marks
  impersonated rows). Deferred: a failed-login rate-limit counter.
