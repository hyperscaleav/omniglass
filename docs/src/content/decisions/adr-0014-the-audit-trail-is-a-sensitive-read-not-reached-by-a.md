---
title: "The audit trail is a sensitive read, not reached by a partial global wildcard"
date: 2026-07-07
status: "Superseded by [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)"
summary: "The audit trail is admin/owner-only: `audit` is a sensitive resource that `*:read` does not confer, only an explicit `audit:read` or `*:*`"
---

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Reading the audit trail requires the `audit:read` capability, and `audit` is a **sensitive
  resource**: a partial global wildcard (`*:<action>`, e.g. the `viewer` role's `*:read`) does **not** confer
  it. Only an explicit grant on the resource (`audit:read`, held by `admin`) or the full `*:*` superuser
  wildcard (held by `owner`) reaches it. So the audit trail is admin/owner-only; a read-only user does not see
  logins, impersonations, and access changes (issue [#116](https://github.com/hyperscaleav/omniglass/issues/116)).
- **Context:** The `:read` floor and the `*:read` viewer role mean "read everything," which is right for the
  estate but wrong for the security audit trail: exposing who impersonated whom and every access change to any
  read-only operator leaks security posture. Rather than gate the route with a non-read action (a hack), `rbac`
  gains a small **sensitive-resource** set: in `Set.Allows`, a `*` resource entry that is not `allActions` skips
  a sensitive resource, so `*:read` no longer matches it while `*:*` still does and an explicit `audit:read`
  still does. This is the narrow, honest version of the "sensitive permission" idea (distinct from the
  impersonate call in [ADR-0012](#adr-0012-owner-accounts-are-un-impersonatable-impersonation-stays-capability-gated-not-scope-intersected),
  where the `principal:*` **resource** wildcard legitimately confers `principal:impersonate`; here it is the
  **global** `*:read` wildcard over a sensitive **read**). The set is extensible if other sensitive reads
  appear (it holds only `audit` today).
- **Closes the gap:** issue [#116](https://github.com/hyperscaleav/omniglass/issues/116).
- **Superseded by** [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards): the
  carve-out is replaced by consistent topic-pattern matching, where `:admin` is a deeper token no partial
  wildcard reaches.
