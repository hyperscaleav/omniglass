---
title: "Permissions are topic patterns (single-token and tail wildcards)"
date: 2026-07-07
status: "Accepted"
summary: "Permissions match like NATS subjects (`*` one token, `>` tail); admin-sensitivity is a deeper `:admin` token no partial wildcard reaches; owner is `>`"
---

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Permissions match like **NATS subjects** (which the node path already uses, so the stack shares
  one wildcard convention): a colon-delimited token path where a literal matches itself, **`*` matches exactly
  one token**, and **`>` matches one or more tokens and must be last**. A normal permission is
  `resource:action`; an admin-sensitive one is `resource:action:admin`. Because `*` is a single token, a
  two-token pattern (`*:read`, `*:*`, `principal:*`) structurally cannot match a three-token `:admin`
  permission: admin-sensitivity is a **deeper token**, not a special case. The whole-estate superuser is `>`
  (issue [#118](https://github.com/hyperscaleav/omniglass/issues/118)).
- **Context:** The prior ad-hoc wildcard let a two-token `*:*` match a three-token `x:y:z`, an inconsistency:
  the second `*` was silently absorbing a tail. Making matching a real topic match removes every special case,
  the [ADR-0014](#adr-0014-the-audit-trail-is-a-sensitive-read-not-reached-by-a-partial-global-wildcard)
  `sensitiveResources` set is **deleted**. `viewer`'s `*:read` misses `audit:read:admin` because two tokens
  cannot match three; `owner` reaches it via `>`; `admin` carries `audit:read:admin` explicitly. It also fixes,
  for free, a boundary wart from the [grant guard](#adr-0013-a-grant-cannot-confer-capabilities-the-granter-lacks):
  `principal:*` is now `principal:<one token>`, so it does **not** sweep an admin-tier `principal:<action>:admin`,
  those stay owner-only unless granted explicitly. `Set.Allows` matches by token; `Set.Covers` (the impersonation
  and grant-escalation guard) becomes pattern subsumption plus the `:read` floor, staying conservative (a reach
  covered only by the union of several patterns returns false, deny). The only seed change is `owner`'s `*:*`
  becoming `>`; every other permission keeps its meaning because `*` already meant a single token. A closed
  grammar also makes "what does this pattern set grant" exactly enumerable against a permission **catalog** (the
  set of all `resource:action[:admin]` the routes declare), the basis for a future custom-role preview.
- **Supersedes:** [ADR-0014](#adr-0014-the-audit-trail-is-a-sensitive-read-not-reached-by-a-partial-global-wildcard).
- **Closes the gap:** issue [#118](https://github.com/hyperscaleav/omniglass/issues/118).
