---
title: Decision log
description: "The dated history of architectural calls: reversals, settled open questions, and where the build currently diverges from the present-tense design."
---

The architecture pages are written in the present tense as the **target design**, and each carries a
status badge that says how much of it is built ([implementation status](/architecture/status/)). Neither
axis carries **history**: why a call was made, when it was reversed, or why the shipped code differs from
the page that describes it. That is what this log is for.

A page tells you what the design **is** and how much is built. This log tells you how it **got there**:
the decisions that bind the design, the ones that were reversed in the open, and the points where the
implementation has deliberately (or accidentally) drifted from the prose. It is the project's
architecture decision record (ADR), kept lightweight and append-only.

## How it works

- **One entry per decision** that reverses a prior call, settles an [open question](/architecture/status/),
  or records a point where the build diverges from a page's present-tense design.
- Each entry carries a **date**, a **status** (`Proposed`, `Accepted`, or `Superseded`), the **decision**
  in one line, the **context** that forced it, and the **page(s)** it touches.
- A **divergence** entry is the partner of a page's inline note: the page says what is true *now*, this
  log says *why* and *when* it diverged, and which issue tracks closing the gap.
- Entries are **never edited away**. A reversed decision is marked `Superseded` and points at the entry
  that replaced it, so the trail of reasoning survives. Nothing in this log is deleted when a page
  changes.
- New reversals and divergences are added **per slice**, as part of the
  [ship gate](/contributing/slice-workflow/): if a slice changes a settled call or ships something that
  differs from its architecture page, the entry lands in the same PR.

This log was seeded on 2026-06-30 from the first architecture-drift review, which backfilled the entries
below from the project's history. From here it grows one slice at a time.

## Index

| ID | Date | Status | Decision |
|---|---|---|---|
| [ADR-0001](#adr-0001-ai-acts-as-a-user-the-agent-principal-is-deferred) | 2026-06-27 | Accepted | AI acts as a `human` / `service` principal; a first-class `agent` principal is deferred |
| [ADR-0002](#adr-0002-roles-carry-requirements-not-an-allow-list) | 2026-06-27 | Accepted | Authorization is role + scope grants, not a per-principal allow-list |
| [ADR-0003](#adr-0003-health-reads-ok-not-up) | 2026-06-27 | Accepted | The healthy state is named `ok`, not `up` |
| [ADR-0004](#adr-0004-credentials-ship-bearer-only) | 2026-06-27 | Resolved | Bearer shipped first; `password` credentials (argon2id) landed in identity slices 1-2. OIDC / NATS still deferred |
| [ADR-0005](#adr-0005-the-first-owner-is-omniglass-bootstrap) | 2026-06-27 | Resolved | `omniglass bootstrap <username> [--password]`; the password-on-create path shipped, the `iam` namespace is deferred |
| [ADR-0006](#adr-0006-the-owner-invariant-is-enforced-by-bootstrap-for-now) | 2026-06-27 | Resolved | The single-owner invariant is now a DEFERRABLE constraint trigger, landed with grant revocation |
| [ADR-0007](#adr-0007-principals-are-gated-at-all-scope-not-scope-tree) | 2026-07-01 | Accepted | A principal is not a scope-tree entity; the `principal` capability confers access only at all-scope |
| [ADR-0008](#adr-0008-disable-is-hard-revocation-no-token-version-column) | 2026-07-06 | Accepted | Disable revokes live sessions via the per-request `active` re-read; no token-version column (nothing consumes it) |
| [ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind) | 2026-07-06 | Superseded by [ADR-0011](#adr-0011-grant-scope-is-an-operator-not-a-boolean-modifier) | The deploy "act on the subtree but not the root" capability is an `exclude_root` grant modifier, not a new scope kind |
| [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover) | 2026-07-06 | Accepted | Impersonation ships view-as + act-as as an `impersonation_session` (not a credential), guarded by capability-cover, with a real-actor audit column |
| [ADR-0011](#adr-0011-grant-scope-is-an-operator-not-a-boolean-modifier) | 2026-07-06 | Accepted | Generalize the `exclude_root` boolean into a `scope_op` operator (`subtree` / `subtree_excl_root` / `self`), a flat enum, not a predicate-expression tree |
| [ADR-0012](#adr-0012-owner-accounts-are-un-impersonatable-impersonation-stays-capability-gated-not-scope-intersected) | 2026-07-07 | Accepted | Owner accounts are un-impersonatable by anyone; impersonate stays swept by `principal:*`; drop act-as scope intersection (#101) |
| [ADR-0013](#adr-0013-a-grant-cannot-confer-capabilities-the-granter-lacks) | 2026-07-07 | Accepted | Grant creation is refused when the granted role's capabilities exceed the granter's all-scope capabilities (admin cannot self-promote to owner) |
| [ADR-0014](#adr-0014-the-audit-trail-is-a-sensitive-read-not-reached-by-a-partial-global-wildcard) | 2026-07-07 | Superseded by [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards) | The audit trail is admin/owner-only: `audit` is a sensitive resource that `*:read` does not confer, only an explicit `audit:read` or `*:*` |
| [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards) | 2026-07-07 | Accepted | Permissions match like NATS subjects (`*` one token, `>` tail); admin-sensitivity is a deeper `:admin` token no partial wildcard reaches; owner is `>` |
| [ADR-0016](#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it) | 2026-07-09 | Accepted | A principal can be hard-deleted (purge, gated on archival); the audit trail survives via a denormalized actor label and `ON DELETE SET NULL`, retiring the "never hard-deleted" rule (soft-delete verb: archive) |

## Entries

### ADR-0001: AI acts as a user; the `agent` principal is deferred

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [AI](/architecture/ai/)
- **Decision:** An AI tool authenticates over OAuth as an ordinary `human` or `service` principal and acts
  with exactly that principal's grants. A dedicated first-class `agent` principal kind is **not** in the
  initial architecture.
- **Context:** A separate `agent` identity would need its own authN, its own grant semantics, and its own
  audit treatment before any AI surface exists to use it. Treating AI as a scoped, audited user reuses the
  whole identity machinery and keeps the audit trail honest (the acting principal is the human or service).
- **Note:** The schema's `principal.kind` CHECK already **reserves** the `agent` value so a later slice
  adds the kind without editing the applied auth migration; no `agent` identity is issued today. If and
  when a first-class agent identity is built, that is a new entry that supersedes this one.

### ADR-0002: Roles carry requirements, not an allow-list

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Authorization is built from additive `(role x scope)` grants, where a role is a capability
  set of `<resource>:<action>` permissions. An earlier sketch attached a per-principal **allow-list** of
  permitted actions directly.
- **Context:** A per-principal allow-list does not compose: the same operator role at two scopes, or a role
  inherited and extended, would be re-listed by hand per principal. Roles plus scope make the common case
  (the same role at different places) a single reused definition, and keep permissions additive and
  positive (no negative entries). It is also what makes the per-grant binding (an action and its scope bind
  in the *same* grant) expressible.

### ADR-0003: Health reads `ok`, not `up`

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [health](/architecture/health/)
- **Decision:** The healthy state of a component or system is named **`ok`**. An earlier draft used `up`.
- **Context:** `up` reads as reachability (the device answers), which is only one input to health. Health is
  a rollup verdict ("is this system working?") that can be unhealthy while every device is reachable, or
  healthy while a redundant member is down. `ok` names the verdict rather than the ping, so the word does
  not promise something narrower than the model delivers.

### ADR-0004: Credentials ship bearer-only

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

### ADR-0005: The first owner is `omniglass bootstrap`

- **Date:** 2026-06-27 | **Status:** Resolved (identity slice 1) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** `omniglass bootstrap <username> --password <pw>` shipped in identity slice 1 ([#35](https://github.com/hyperscaleav/omniglass/pull/35)): bootstrap now installs a password credential on create (plus `--email` / `--display-name`), so the owner can sign in to the console without a separate step. The `og iam` admin command namespace is still deferred (it lands with the admin user surface, slice 3).
- **Decision (divergence):** The first owner is created by `omniglass bootstrap <username>`, which mints an
  `owner@all` grant plus a **bearer** credential in one transaction. The design page describes the eventual
  `og iam create-owner --username ... --email ...` password path under an `iam` command namespace; that
  namespace and the password credential are **deferred**.
- **Context:** Bootstrap has to work before any login surface exists, so it pairs with the bearer-only
  credential decision (ADR-0004): one trusted, idempotent command that produces a token the operator pastes
  into the console or CLI. The `iam` command family (and the password-on-create path) lands with the
  identity-tier admin surfaces.
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).

### ADR-0006: The owner invariant is enforced by bootstrap for now

- **Date:** 2026-06-27 | **Status:** Resolved (identity slice 3c) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** The `DEFERRABLE INITIALLY DEFERRED` constraint trigger (`principal_grant_owner_guard`) shipped with grant revocation ([issue #82](https://github.com/hyperscaleav/omniglass/issues/82)): it refuses to leave zero `owner @ all` grants at `COMMIT`, so revoking the last owner is a clean 409 while a swap (grant a new owner + revoke the old in one transaction) still passes. The gateway maps its custom SQLSTATE `OG001` to `ErrLastOwner`.
- **Decision (divergence):** "At least one active `owner@all` grant exists at all times" is upheld today by
  the bootstrap path (it always creates one) and the absence of any grant-revocation surface. The design's
  **deferrable Postgres constraint trigger** that enforces it at `COMMIT` (so the swap-owners-in-one-txn
  pattern works) is **not yet built**.
- **Context:** With no API to revoke a grant or delete a principal, the last-owner removal the trigger
  guards against is not yet reachable, so the trigger is not load-bearing until grant CRUD ships. It is
  required before the admin user-management slice exposes grant revocation
  ([epic #27](https://github.com/hyperscaleav/omniglass/issues/27), slice 3).
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).

### ADR-0007: Principals are gated at all-scope, not scope-tree

- **Date:** 2026-07-01 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A `principal` is not a scope-tree entity: it is not "under" a location, system, or component,
  so the `principal:<action>` capability confers access **only at all-scope**. A grant scoped to a location
  or system carries no principal access, and the Storage Gateway refuses a non-all scope on the principal
  directory with a 403 (`ErrPrincipalForbidden`) rather than silently returning an empty list. This falls out
  of the scope resolver: `applicableKinds("principal")` is empty, so only an `all` grant resolves to a
  non-empty set.
- **Context:** The admin principal directory (slice 3a, [issue #77](https://github.com/hyperscaleav/omniglass/issues/77))
  is the first surface to gate on `principal:*`. Modelling users as scope-tree entities would be wrong (there
  is no "users under HQ"), and returning an empty list to a mis-scoped admin would hide a misconfiguration, so
  making all-scope explicit keeps the capability honest and surfaces the error. The same rule governs the later
  principal-mutation and grant surfaces.
- **Closes the gap:** n/a (a design decision, not a divergence).

### ADR-0008: Disable is hard revocation; no token-version column

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Disabling a principal revokes its live sessions immediately, achieved by the authn path
  re-reading `principal.active` on **every** request, not by a session-version / epoch column.
  `AuthenticateBearer` and `AuthenticatePassword` both filter `and pr.active` in the credential lookup on
  every call, with no caching anywhere in the authn path, so the very next request on an already-issued
  bearer or session cookie after a disable gets zero rows and a 401. `SetPrincipalActive` flips the flag in
  one statement: disable **is** revocation, atomically. No `token_version` column is added.
- **Context:** Issue [#94](https://github.com/hyperscaleav/omniglass/issues/94) asked for "hard session
  revocation on disable", assuming disable was soft (a propagation delay). It is not: the per-request active
  check already is the hard-revocation mechanism, proven end to end by `TestDisableRevokesLiveSessionAPI` (a
  live token is 401 on its next request the moment it is disabled) and `TestDisablePrincipal`. A
  `token_version` column would matter only as an invalidation signal for an authn-result cache, which does
  not exist; adding it now would be a dead column with no reader, against the primitive-first and
  meaningful-migration disciplines. Revisit if any cache/memoization is introduced in the authn path (an
  epoch bump would then be its invalidation signal).
- **Closes the gap:** issue [#94](https://github.com/hyperscaleav/omniglass/issues/94), closed as already satisfied.

### ADR-0009: Root exclusion lives on the grant, not a new scope kind

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** The "act on the subtree but not the root" capability (the deploy / integrator case, issue
  [#87](https://github.com/hyperscaleav/omniglass/issues/87)) is a boolean `exclude_root` modifier on
  `principal_grant`, not a new `scope_kind` (e.g. `location_descendants`) and not a role-level flag. It narrows
  only the **modify** actions (update, delete) to the root's descendants; read and create-placement keep the
  root. An inclusive grant on the same root wins over an excluding one.
- **Context:** A new scope_kind would fork the kind handling three ways (location / system / component) and
  grow the scope vocabulary; a role-level flag could not vary per grant (the same deploy role granted
  root-inclusive in one place and root-excluded in another). The grant modifier composes with the
  additive-grant model and confines the change to one predicate (`inScopeTree`) shared by all three tree
  entities. Keeping read and create-placement inclusive means a `PATCH` on the root is the existing
  readable-but-out-of-write-scope 403, so `exclude_root` reuses the three-way status split rather than adding a
  fourth case. Shipped with a new `deploy` official role (create + update on the three tree tiers, read via the
  viewer floor). The grant-builder toggle to set it from the console is a fast-follow ([#99](https://github.com/hyperscaleav/omniglass/issues/99)).
- **Closes the gap:** issue [#87](https://github.com/hyperscaleav/omniglass/issues/87).

### ADR-0010: Impersonation is a session, not a credential; guarded by capability cover

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Admin/owner impersonation ships with **both** modes (view-as read-only, act-as full) in one
  slice. An impersonation token is an `impersonation_session` row (its own table: target, real actor, mode,
  expiry, revoke), **not** a `credential` (which authenticates a principal as itself). Authorization to
  impersonate is the escalation guard `actor.Covers(target)` (the caller's capabilities must cover the
  target's) plus the `principal:impersonate` capability at all-scope. Capability cover applies to both modes;
  **scope** is where the modes diverge: **view-as** is cross-scope (read-only grants no write authority, and
  seeing another scope is the troubleshooting case), but **act-as** additionally requires the caller's
  **all-scope grants alone** to cover the target: a capability held only through a narrower grant does not
  count. Without that, act-as would let a split-grant admin (all-scope user management, but infra scoped to
  campus X) impersonate a campus-Y admin and gain write in Y, since an impersonated request resolves its ABAC
  scope from the target: a scope escalation. Because the rule is capability-cover against the caller's
  all-scope grants (not a hardcoded list of scoped resources), it closes non-tree escalation too: a user-admin
  who holds grant authority only through a scoped grant (empty effective scope, cannot create a grant directly)
  cannot launder all-scope grant authority by acting-as a grant admin. Accountability
  is a nullable `audit_log.real_actor_principal_id` written on the row directly, not reconstructed from a
  time-window join (clock skew and concurrent sessions make that unreliable for an accountability record), and
  the self-service mutations (`/auth/me` profile and password) audit too so an act-as edit is never untracked.
- **Context:** view-as is enforced by refusing every non-read action when the request carries a view-as
  claim; act-as threads the real actor through the audit writer via a request-scoped context value
  (`storage.WithRealActor`), so no mutating gateway signature changes. `authn` tries the impersonation session
  on a bearer-hash miss, so the same `Authorization: Bearer` path serves both. Disabling either party kills
  the session via the per-request `active` re-read ([ADR-0008](#adr-0008-disable-is-hard-revocation-no-token-version-column)).
  The console ships an Impersonate action (view-as / act-as) and an acting-as banner. Deferred: re-checking
  the escalation guard on every request (bounded instead by a short TTL plus revoke), and act-as within a
  scoped admin's own scope by intersecting the target's scope with the caller's ([#101](https://github.com/hyperscaleav/omniglass/issues/101)),
  rather than the current all-scope-only act-as rule.
- **Closes the gap:** issue [#85](https://github.com/hyperscaleav/omniglass/issues/85).

### ADR-0011: Grant scope is an operator, not a boolean modifier

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Generalize the `exclude_root` boolean ([ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind))
  into a `scope_op` operator on `principal_grant` (issue [#102](https://github.com/hyperscaleav/omniglass/issues/102)):
  `subtree` (root + descendants, the default, == old `exclude_root=false`), `subtree_excl_root` (descendants
  only for update/delete, root kept for read/create, == old `exclude_root=true`), and `self` (the root row
  only for read/update/delete, no descendants and no create-placement, a leaf-lock, net-new). The operator is a **flat enum column**, not a full predicate-expression
  tree or a per-grant tuple list. It is part of a grant's identity: the dedup index includes `scope_op`, so the
  same role at the same root with a different operator is a distinct grant.
- **Context:** Grant scope wants one composable axis, not a growing pile of booleans; the grant builder is
  already a filter-bar-style operator UI, so the operator vocabulary is the natural fit. The flat enum was
  chosen over a predicate-expression scope and a per-grant tuple list (negation, multi-root `in`): those buy
  expressiveness the boolean's two states never needed, at the cost of a much larger blast radius on the two
  authorization invariants (permission-on-every-route, scope-on-every-query). `self` is the cheap third value
  (a scalar `= any()` arm, no new recursive CTE) that turns a boolean rename into a real operator, and grant on
  exactly one node is a frequently-wanted capability the boolean could never express. The pure `scope.Resolve`
  gains a `SelfIDs` set; the three gateway walks (`inScopeTree`, `InScopeIDs`, `scopedListSQL`) gain a self arm.
  The migration also recreates the dedup index to include `scope_op`, fixing a latent collision, and threads
  `scope_op` through `RevokeGrant`'s audit SELECT (previously dropped). The operator model does **not** subsume
  the act-as scope intersection ([#101](https://github.com/hyperscaleav/omniglass/issues/101)): that blocker is
  plumbing (carry the real actor's grants and intersect two Sets per row), unchanged by how a Set is expressed.
  A future tuple model (negation, multi-root) stays a documented path if a real carve-out requirement lands.
  The console grant builder gains an operator stage (role -> kind -> entity -> operator), so [#99](https://github.com/hyperscaleav/omniglass/issues/99)
  (setting the modifier from the console) ships here too.
- **Supersedes:** [ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind) (the boolean is retired for the operator).
- **Closes the gap:** issue [#102](https://github.com/hyperscaleav/omniglass/issues/102).

### ADR-0012: Owner accounts are un-impersonatable; impersonation stays capability-gated, not scope-intersected

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Harden the impersonation authorization model on tiers, not scope. (1) A principal holding
  `owner @ all` cannot be impersonated by **anyone**, including another owner, in either mode (issue
  [#106](https://github.com/hyperscaleav/omniglass/issues/106)): a target-side check in the `:impersonate`
  handler, before the mode branch. (2) The `principal:impersonate` capability stays **swept by the
  `principal:*` wildcard** (admin) and `*:*` (owner); it is not carved out as a sensitive action, because
  holding `principal:*` already lets a caller create and use its own principals, so impersonate confers no new
  reach there. (3) **Drop** act-as scope intersection ([#101](https://github.com/hyperscaleav/omniglass/issues/101)):
  act-as stays all-scope-only.
- **Context:** The escalation guard (`Covers`) already blocks a lesser admin from impersonating an owner, but
  `owner.Covers(owner)` is true, so owner-impersonates-owner was possible. An owner is the highest-trust
  account and impersonating one is a full-takeover vector, so the explicit owner-protection rule removes it
  entirely and reads more clearly than relying on cover arithmetic. Owner detection reuses the same
  `role='owner' and scope_kind='all'` lane as the [owner invariant](#the-owner-invariant), so it is not new
  role-name branching. Scope intersection (a scoped admin acting-as within its own subtree by intersecting two
  scope Sets per row) was dropped as complexity for a narrow case; the tier model plus all-scope-only act-as is
  simpler and safe. The impersonated-vs-direct distinction an operator needs in the audit trail is already
  recorded by `audit_log.real_actor_principal_id` ([ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover));
  surfacing it is a later auth-event audit slice.
- **Refines:** [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover).
- **Closes the gap:** issue [#106](https://github.com/hyperscaleav/omniglass/issues/106); closes [#101](https://github.com/hyperscaleav/omniglass/issues/101) as dropped.

### ADR-0013: A grant cannot confer capabilities the granter lacks

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Grant creation is refused (403) when the granted role's capabilities are not covered by the
  granter's **all-scope** capabilities (`rbac.Set.Covers`, the same primitive as the impersonation escalation
  guard). So no caller can promote anyone, including itself, to a tier above its own: an **admin cannot grant
  `owner`** (`*:*`), because admin is an enumerated role that does not cover the global wildcard. Issue
  [#109](https://github.com/hyperscaleav/omniglass/issues/109).
- **Context:** `CreateGrant` previously checked only that the granter held all-scope `principal_grant:create`
  (`action.All`), not that the granter covered the granted role, so an admin could grant itself `owner@all` and
  log in as a superuser, leaving the admin/owner distinction unenforced. The check lives in the `create-grant`
  handler (capability is a route/handler concern; ABAC scope stays the gateway's), mirroring the impersonation
  guard. Only the caller's **all-scope** grants count, so a capability held through a narrower grant cannot be
  conferred estate-wide (the same reason act-as requires all-scope cover). The consequence is a deliberate
  stance: **admin is bounded on purpose**, the top management role, never the superuser, and does not auto-gain
  future resources; `owner` (`*:*`) is the break-glass superuser and the [owner-invariant](#the-owner-invariant)
  anchor. The same cover rule must extend to role editing when it lands (you cannot edit a role above your own
  tier); tracked with that slice.
- **Refines:** [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover) (reuses its capability-cover primitive on the grant path).
- **Closes the gap:** issue [#109](https://github.com/hyperscaleav/omniglass/issues/109).

### ADR-0014: The audit trail is a sensitive read, not reached by a partial global wildcard

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

### ADR-0015: Permissions are topic patterns (single-token and tail wildcards)

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

### ADR-0016: A principal can be purged, and the audit trail is denormalized to survive it

- **Date:** 2026-07-09 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A principal gains a full **lifecycle**: **disable** (reversible, the `active` flag),
  **archive** (a soft delete, `archived_at`, hidden from the directory and unable to authenticate,
  reversible), and **purge** (an irreversible hard delete of the row). Purge is **gated on prior archival**
  (archive-before-delete) and on the admin-sensitive `principal:purge:admin`, so `admin` (which carries it
  explicitly) and `owner` (`>`) can purge but a two-token `principal:*` cannot reach it
  ([ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)). To keep the audit
  trail through a hard delete, the actor's human-readable label is **denormalized** into every `audit_log` row
  at write time, and the audit foreign keys become `ON DELETE SET NULL`: a purge nulls the id link but the text
  survives, so "who did X" outlives the principal. The read side coalesces the live join to the snapshot.
- **Context:** [ADR-0006](#adr-0006-a-single-owner-invariant-enforced-at-the-database)'s single-owner invariant
  meant accounts were **disabled, never hard-deleted**, since audit rows referenced them (`RESTRICT`). But
  operators need to remove accounts created by mistake, a common task, without erasing history or orphaning the
  trail. Denormalizing the actor label decouples the audit record from the principal row, so the row can be
  purged while the history stays legible; the archive gate prevents an accidental one-click hard delete, and
  the last-active-owner guard (extended to archive) means a purgeable account is never the last owner. This
  retires the "never hard-deleted" statement in the identity-access page.
- **Naming:** the soft-delete verb was renamed **deactivate to archive** (and reactivate to **restore**) when
  the console UI landed ([#146](https://github.com/hyperscaleav/omniglass/issues/146)): "disable" and
  "deactivate" read as synonyms, blurring two distinct operations. The ladder is now a *suspend* (**disable**,
  reversible, still listed) then an *offboard* (**archive**, soft delete, hidden, recoverable) then a *destroy*
  (**purge**), so the labels read pause to remove to destroy, matching the industry suspend-vs-delete pair. The
  column, endpoints (`:archive` / `:restore`), capability (`principal:archive`), and list param
  (`include_archived`) all follow the verb.
- **Closes:** issue [#143](https://github.com/hyperscaleav/omniglass/issues/143) (backend),
  [#146](https://github.com/hyperscaleav/omniglass/issues/146) (console + rename).

### ADR-0017: `credential` is renamed `secret`; the cascade is the reuse mechanism

- **Date:** 2026-07-09 | **Status:** Accepted | **Pages:** [config, credentials, and variables](/architecture/variables/)
- **Decision:** The access-secret member of the [config / credential / variable](/architecture/variables/) trio
  is renamed **credential to secret**, and its first slice is built: a typed, encrypted-at-rest value owned on the
  exclusive arc (`global | location | system | component`) and resolved most-specific-wins down the
  [cascade](/architecture/cascade/). A secret is an **encapsulated typed cell** (a `secret_type` shape with
  per-field secrecy and origin), not a bag of references: the reuse a tool like Windmill gets from variable
  references, **the cascade already provides here** (define once at a broad scope, inherit it below), so
  composition solves a non-problem. Interpolation references live at the **consumption site** (`$sec:name.path`
  in an interface input or a function arg), never inside a secret's own fields. Crypto is **envelope AES-256-GCM**
  behind a pluggable KEK provider (env / file / fallback), the value sealed under a per-value DEK wrapped by the
  KEK, with `(owner, name, field)` bound as AAD; the provider seam lets a KMS or Vault drop in without a model
  change. "credential" is retained for the **authentication** credential (a principal's bearer or password), a
  distinct resource; only the collection-side access secret is renamed.
- **Context:** The written [variables](/architecture/variables/) page named this member `credential` and left it
  `Design`. Building it surfaced two calls. First, **naming**: "credential" collided with the identity
  credential and undersold the general case (an `snmp_community`, an API key, an `oauth2` blob are all just
  sensitive cascaded values); "secret" is the Cloudflare-style vars-and-secrets pair and reads correctly. Second,
  **shape**: Windmill's resource-references-variables split was considered and rejected, because our cascade is
  the sharing mechanism and an atomic one-form typed cell (doctrine 4) suits an operator better than composing
  references. Reveal (plaintext decrypt) ships as an audited, `secret:reveal`-gated endpoint that the `*:read`
  floor does not reach, so only admin and owner may decrypt; the interpolation consumer (splicing a value into a
  live request) is deferred to the collection-driver slice that first needs it. This reverses the `credential`
  naming and any "references inside the value" reading on the page; the `variable` and `config` members stay
  `Design`.
- **Closes:** issue [#155](https://github.com/hyperscaleav/omniglass/issues/155) (secret slice 1).

### ADR-0018: `variable` slice 1 types inline and mirrors the secret arc

- **Date:** 2026-07-11 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** The **variable** member of the trio ships its first slice: a typed, cascade-resolved **plaintext**
  value owned on the exclusive arc and resolved most-specific-wins down the [cascade](/architecture/cascade/), with a
  Variables directory and a per-component effective-variables panel, mirroring the [secret](#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism)
  member minus crypto, masking, and the reveal. `variable:create,update` is granted to **operators** (delete stays
  admin and owner), the same split secret got. Three parts of the written design are deferred to keep the slice one
  vertical cut. First, **typing is inline**: a `value_type` enum (`string | int | float | bool | json`) on the row
  plus a jsonb `value` validated against it in a pure `internal/variable` package, **not** a `variable_type` shape
  registry. A scalar needs no governed vocabulary, and the page itself calls variables the "operator-defined, not
  curated" member, so a registry would contradict the model. Second, the **`template` owner scope** (the design's
  `global -> template -> instance`) is out: slice 1 mirrors the secret arc (`global | location | system | component`),
  and template scope plus cascade groups land together in [#184](https://github.com/hyperscaleav/omniglass/issues/184),
  because they touch the shared resolver once for both members. Third, the **`$var:` consumer** and the
  **secret-flagged** variable are deferred (the consumer has no live interpolation site yet, as with `$sec:`).
- **Context:** The written [variables](/architecture/variables/) page sketched a `variable_type` registry and a
  shared config/variable cell carrying `observed_value` and `reconcile`. Building the member showed those belong to
  **config** (the declared-vs-observed member), not the free macro: a variable has no observed side. So `variable`
  shipped as its own single table, typed inline, and the page's Storage section is corrected to match. This diverges
  from the page's `variable_type`-registry and shared-cell sketch; the `config` member stays `Design`.
- **Closes:** issue [#183](https://github.com/hyperscaleav/omniglass/issues/183) (variable slice 1).
