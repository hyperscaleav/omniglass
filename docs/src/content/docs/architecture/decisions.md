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
| [ADR-0016](#adr-0016-a-node-is-a-kindnode-principal-with-an-interim-bearer-credential-and-static-per-connection-nats-subject-permissions) | 2026-07-07 | Accepted | A node is a `principal` of `kind=node` with a 1:1 detail table and a bearer `credential` row (interim shared secret), and per-node NATS isolation is static per-connection subject permissions via an in-process auth callback; nkey/JWT deferred |
| [ADR-0017](#adr-0017-telemetry-is-a-protobuf-event-over-jetstream-with-an-inline-owner-confining-consumer) | 2026-07-07 | Accepted | Telemetry is a protobuf `Event` over a JetStream durable consumer; the consumer binds the owner from the task's interface and confines a node to its own tasks inline (no separate raw-telemetry table or Postgres queue); raw persistence + replay and label-based multi-owner routing deferred |

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

### ADR-0016: A node is a kind=node principal with an interim bearer credential and static per-connection NATS subject permissions

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [identity and access](/architecture/identity-access/)
- **Decision:** A node is a first-class `principal` of `kind='node'` with a 1:1 `node` detail table (keyed by
  `principal_id`, alongside `human` and `service`), exactly as [identity and access](/architecture/identity-access/)
  describes. Its `name` is `not null unique` on the detail table and stays the estate address the collection FKs
  (`interface.node_name`, `task.node_name`, `metric_datapoint.node_id`) reference. The node runtime ships with
  two deliberate calls that diverge from the present-tense design, both reversible in a later hardening slice.
  (1) The node's credential is a **bearer `credential` row** on its principal, minted, stored (only as
  `sha256`), and verified through the **same helpers a service bearer token uses** (`AuthenticateBearer`), and
  the enrollment token **doubles as the node's NATS password** (a shared secret), rather than being a single-use
  bootstrap exchanged for a distinct long-lived credential. The decentralized **nkey/JWT operator-account**
  model that identity and access describes for nodes (a `nats` credential kind, a signed nonce, a JWT carrying
  the node's subject permissions) is deferred; the `credential` kind CHECK is **not** widened for it here. (2)
  Per-node NATS isolation is **static per-connection subject permissions**: the embedded `nats-server` runs an
  in-process `CustomClientAuthentication` callback that resolves each connecting node by name, verifies its
  bearer credential, and registers a user whose publish/subscribe grants are scoped to that node's own
  `og.v1.*.<node>` subjects, so a node cannot publish or pull as another.
- **Context:** Checkpoint 2 of the reachability slice needed a real, negatively-tested per-node isolation
  mechanic against an embedded server, without carrying the full JWT/nkey machinery a single slice should not.
  The auth-callback path adds per-node users **dynamically at enrollment time with no config reload**, which is
  the simplest mechanism that keeps the isolation invariant real: the negative test proves node A cannot use
  node B's subjects (and a confused-deputy reply cannot forge another node's liveness), and a wrong credential
  is rejected at connect. The subject encodes the node name in its last token and the callback grants only that
  node's subjects, so the subject **is** the transport isolation boundary (the payload-owner admission fence is
  a later checkpoint). Modeling the node as a `kind=node` principal (rather than the standalone table an earlier
  checkpoint built) puts it on the shared identity spine from the start: it has a real `principal_id` so it can
  be an audit actor, its credential rides the audited human/service machinery, and only the credential *scheme*
  (interim bearer vs nkey/JWT) remains to tighten. JetStream is enabled on the server now (it boots and shuts
  down cleanly), but the control-plane messages (worklist, heartbeat) are JSON over core NATS; the protobuf
  telemetry `Event` over JetStream is the next checkpoint.
- **Closes the gap:** the nkey/JWT node identity (the `nats` credential kind and the signed-nonce admission)
  and the single-use enrollment token are tracked with the node-identity hardening slice.

### ADR-0017: Telemetry is a protobuf Event over JetStream with an inline owner-confining consumer

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [datapoints](/architecture/datapoints/)
- **Decision:** A node ships each collected batch as a protobuf `Event` (proto3, `proto/og/v1/event.proto`,
  `Event` + `Datapoint` messages only, no gRPC service) published to `og.v1.telemetry.<node>`. This is
  omniglass's first protobuf; the wire is generated with `protoc` + `protoc-gen-go` via a `gen-proto` stage on
  `make gen`, and the generated `event.pb.go` is committed. The server hosts a JetStream stream
  (`OG_TELEMETRY` over `og.v1.telemetry.*`) and a **single durable consumer** (`og-telemetry-worker`,
  AckExplicit) whose handler, per Event, **derives and writes inline**: it decodes the batch, resolves the
  owner as the task's interface component, **confines** the node to its own tasks, applies reject-not-project
  against the `datapoint_type` registry, writes the surviving typed rows through the checkpoint-1
  `InsertMetricDatapoints` path (`owner_kind=component`, `provenance=observed`), and acks. A permanent
  condition (an undecodable payload, or an orphan the confinement fence drops) is terminated/acked so it is not
  redelivered; only a transient failure (a DB error) is left unacked so JetStream redelivers. **The node stamps
  no component identity**: its only assertion is the publishing subject (its own name) plus the `task_id`; the
  server binds and confines.
- **Context:** The prior (v2) design split telemetry into a hot path that persisted a raw event to a
  `telemetry` table and an async Postgres queue worker that derived from it. Checkpoint 3 deliberately
  **collapses that split**: the JetStream durable consumer **is** the at-least-once worklist, so there is no
  raw-telemetry table and no Postgres queue in this checkpoint; the handler derives, confines, writes, and acks
  in one place. This keeps the reachability slice small while keeping the two invariants **real and negatively
  tested**: a node cannot land a datapoint for a component it holds no task for (an Event carrying another
  node's `task_id` is orphan-dropped, no row written), and an unregistered datapoint name is dropped, not
  projected. Owner binding is the **interface-prebind path only** (task -> interface -> component); there is no
  separately-authored `transform_rule` (omniglass has none), so label-based multi-owner routing, discovery
  rules, and node-self binding are a later checkpoint.
- **Closes the gap:** raw-`Event` persistence (backfill/replay) and the raw -> admission -> trusted two-lane
  topology, plus label-based multi-owner resolution, are tracked with a later collection checkpoint.
