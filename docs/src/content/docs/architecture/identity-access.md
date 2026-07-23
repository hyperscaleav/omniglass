---
title: Identity and access
description: How principals authenticate, how grants combine roles with scopes, and how the app enforces capability at the route and ABAC scope in the Storage Gateway.
sidebar:
  badge:
    text: Partial
    variant: note
---

Identity and access is how an operator controls who may call the platform and which slice of the estate each caller can see and act on, enforced entirely in the app so "forgot to filter" cannot happen. Enforcement is **two in-app layers**: the capability check (`<resource>:<action>`) runs as **API route middleware** before the handler, and the **ABAC scope** filter is injected by the **Storage Gateway** (the only path to the database), where a row-level filter holds by construction. Scope is built on the cascade's groups ([cascade](/architecture/cascade/)). This doc says what IAM **is**.

:::note[Partial: what is built, and where it diverges]
Built and tested today: the `principal` (+ per-kind `human` / `service`) and `credential` tables, the
`role` / `principal_grant` model, the `audit_log`, the capability fast-reject, **local password auth
(argon2id) behind an httpOnly session cookie** (`POST /auth/login` and `/auth/logout`, the public
`GET /auth/status`), the self-service `GET` / `PATCH /auth/me` and `POST /auth/me:changePassword`, the
self-service **session list and revoke** (`GET /auth/me/sessions`, `POST /auth/me/sessions/{id}:revoke`:
a signed-in user sees their own live bearer credentials, labelled `session` or `token` by their stored
`purpose`, with the current one flagged, and revokes any of them scoped to their own principal so
another principal's credential id is a non-disclosing 404; revoking the current one signs it out), the
admin **session management** (`GET /principals/{id}/sessions`, `POST /principals/{id}/sessions/{sid}:revoke`,
gated by the new `principal:revoke-session` capability: an administrator lists another principal's active
sessions and tokens in the same non-secret shape, with `current` always false, and revokes one, bounded to
the target so a credential id that is not theirs is a non-disclosing 404, behind the same **takeover guard**
as impersonation and the password reset so an owner's sessions cannot be revoked by a lesser admin, and
audited with the admin as the actor), the
admin **principal directory** (`GET /principals`, `GET /principals/{id}`), human **create**
(`POST /principals`) and **update** (`PATCH /principals/{id}`: display name, email, username), an
admin **password reset** (`POST /principals/{id}:resetPassword`, gated `principal:reset-password`,
policy-enforced, no current password, audited as the admin, refused on self (change your own password
from your profile, which verifies your current one), behind the same **takeover guard** as
impersonation so an owner cannot be reset and a caller cannot reset a principal whose capabilities
exceed their own, and **force-logout**: a reset revokes every one of the target's **sessions** so it
takes effect at once (its API **tokens** survive, a token being its own bearer secret, not tied to the
password), and a self-service change revokes the caller's other **sessions** while keeping the current
one and leaving its tokens intact, and **force-change-on-next-login**: a reset sets
`human.must_change_password`, and until the user changes it (which clears the flag) the `authn` choke
point refuses **every** route except reading their own principal and the change itself with a 403, so
the admin-known secret is short-lived and cannot be used to act), **role
assignment** (`POST` / `DELETE /principals/{id}/grants`) with the **owner-invariant trigger** enforcing
that the last `owner @ all` grant cannot be revoked, and the **principal lifecycle**: reversible
**disable** (`POST /principals/{id}:disable` / `:enable`, which refuses authentication for a disabled
principal), a stronger **archive** (`:archive` / `:restore`, a soft delete that hides the account
from the directory and blocks authentication, reversibly until purged), and **purge** (`:purge`, an
irreversible hard delete, gated on prior archival and on the admin-sensitive `principal:purge:admin`);
disabling or archiving the last active owner is refused, and a purge preserves the audit trail by
denormalizing the actor's label into each row (the audit foreign keys go `ON DELETE SET NULL`), so the
history survives even after its actor is gone. And the per-action `visible_set` resolver enforced in the
Storage Gateway across locations, systems, and components, and **principal groups**
(`GET` / `POST` / `PATCH` / `DELETE /principal-groups`, membership, and group grants) whose members
**inherit** the group's grants through the grant-loader union, gated by `principal_group`, and **profile
pictures** (a human's avatar: self-managed via `POST /auth/me:setAvatar` / `:removeAvatar`, admin-managed via
`POST /principals/{id}:setAvatar` gated `principal:set-avatar`, normalized server-side to a 256x256 JPEG and
stored base64 on the human row). Still `Design`:
OIDC / SAML auth, the node / NATS path, the permission cache, custom-role management, and the tenant-policy
lever. The per-slice breakdown is on [implementation status](/architecture/status/).

Where the build currently differs from the present-tense design below (each logged in the
[decision log](/architecture/decisions/)):

- **Credentials are `bearer` or `password`.** `credential.kind` is `bearer` or `password` (argon2id,
  PHC-encoded, one password per principal); the `oidc` / `nats` methods and the full
  `(method, identifier)` lookup are still deferred. The minted bearer token prefix is `ogp_`.
- **Every credential is time-bounded.** `credential.expires_at` bounds a bearer's lifetime, and
  `AuthenticateBearer` treats a passed expiry as absent (the credential authenticates nothing). A **web
  login** installs a session cookie with a fixed absolute lifetime (12h), so a stolen cookie is not valid
  forever, and the cookie's `Max-Age` matches. A CLI-minted **API token** (`omniglass token`) and the
  **bootstrap token** now expire too: a **90-day default** with a `--ttl` override, hard-capped at **365 days**
  (a `--ttl` above the cap errors), so no eternal secret sits in the field ([ADR-0017](/architecture/decisions/#adr-0017-every-credential-is-time-bounded-token-purpose-not-expiry-shape),
  reversing the earlier tokens-never-expire choice). Because both kinds now carry an expiry, they are told
  apart by a **`credential.purpose`** column (`session` vs `token`), not by whether `expires_at` is set.
  Enforcement is **lazy**: an expired row is simply refused at auth, there is no background sweep, and the
  self-service list shows only **live** credentials (`expires_at is null or expires_at > now()`, the same
  filter). A sliding idle timeout, a housekeeping sweep of long-expired rows, and nearing-expiry
  notifications are later refinements.

  | credential | `purpose` | lifetime |
  | --- | --- | --- |
  | web-login session | `session` | 12h absolute (fixed) |
  | CLI/API token (`omniglass token`) | `token` | 90d default, `--ttl` up to 365d max |
  | bootstrap token (`omniglass bootstrap`) | `token` | 90d default, `--ttl` up to 365d max |
- **Failed logins lock the account.** A run of wrong passwords on a real account is throttled by a
  per-username lockout: `human.failed_login_count` counts consecutive misses and, on the 5th, sets
  `human.locked_until` to 15 minutes out. Inside that window `AuthenticatePassword` refuses every
  attempt (even the correct password) with a distinct internal signal that the login handler maps to
  the **same generic 401** as a bad credential, so the lock is not an enumeration oracle; only the
  audit (`login_locked`, attributed to the principal) records it. The lock decision is made **after**
  the argon2 verify, so a locked account is not a measurably faster probe, and a correct password
  below the threshold clears the counter. **Rotating the password clears the lock**: an admin reset
  (`SetPrincipalPassword`) or a self-service change (`SetPassword`) sets `failed_login_count = 0` and
  `locked_until = null` in the same transaction as the new secret, so the account is usable at once
  rather than waiting out the window (the lock otherwise clears only lazily at the next login). The
  threshold and window are fixed for now; per-IP throttling and a configurable policy are later
  refinements.
- **A password policy gates the API password surfaces.** A single pure validator
  (`auth.ValidatePassword`) enforces the policy on the running-server paths that set a password:
  **create a user** (`POST /principals`) and **self-service change-password**
  (`POST /auth/me:changePassword`): **at least 12 characters, not on a common-password denylist, and
  not containing the username**. No character-class composition rules (NIST 800-63B favors length and
  a blocklist). The API maps a violation to 422; the console mirrors the length and username rules
  inline and offers a crypto-strong **Generate** action, while the denylist stays server-side. The
  **direct-DB break-glass lanes** (`bootstrap` and `set-password`) are deliberately **exempt**: they
  already require database access (fully trusted) and are the recovery path, so the policy never
  blocks initial setup or a lockout recovery. A breached-password check (HIBP k-anonymity) is a
  planned enhancement over the embedded list. Because break-glass is a **lockout**, `set-password`
  also revokes the target's live **sessions** (a stolen login stops at once), and revokes its API
  **tokens** too with `--revoke-tokens`. This is the only in-product way to fully cut off a
  compromised **owner**: the API reset and revoke are all 403 on an owner target (the takeover
  guard, owner-to-owner included), so an owner can only be recovered from the direct-DB lane.
- **The `iam` command namespace is not built.** Owner creation is `omniglass bootstrap <username>
  [--password <pw>]` ([Bootstrap](#bootstrap)), not the `og iam create-owner` path; the broader `iam`
  admin CLI is deferred with the admin user surface.
- **The `agent` principal kind** is already reserved in the schema's `kind` CHECK, although no `agent`
  identity is issued yet (AI still acts as a `human` or `service`).
- **The owner invariant** is upheld by the bootstrap path today; the deferrable Postgres trigger
  described under [the owner invariant](#the-owner-invariant) is not yet built.
- **The required permission is published per route in the OpenAPI spec.** Every gated route registers
  through the `gated(op, tokens...)` helper, which stamps the operation with `x-omniglass-permission`,
  so `api/openapi.json` names the capability each request needs. The set of stamps is the **permission
  universe**, which the **Roles view** reports; a role blade renders it as a **net** view (held
  alongside missing, with a `Held / Missing / All` toggle). Two build-time guards keep it honest: the
  published-gate guard (every gated route is stamped, allow-listed routes are not) and a seed-drift
  guard (every seed-role grant resolves into the universe or sits in an `aheadOfRoutes` allow-list).
:::

## The model in one breath

A **principal** is the polymorphic subject of authN/authZ. Identity is the principal's opaque uuid, never an email or name. Each principal has one or more **credentials** (how it authenticates). Each principal holds zero or more **grants**, each a `(role x scope)` pair: the role contributes the verbs, the scope contributes the entities. Permissions are **additive** across grants. The API middleware checks RBAC capabilities before the handler runs; the Storage Gateway injects ABAC scope on every query.

## Principal kinds

A principal carries a `kind` value; the same role machinery works across all kinds. Identity is uniform; authN methods and per-kind domain attributes differ.

| kind | what it represents | authN |
|---|---|---|
| `human` | a person | local password + session, OIDC, SAML |
| `service` | scripts, integrations, SDKs, bots | bearer token |
| `node` | the edge daemon running in the field | NATS JWT/nkey credential |

**AI acts as a user; a first-class `agent` principal is deferred.** An AI tool authenticates via **OAuth as a `human` or `service` principal** and acts with exactly that principal's grants, no separate identity. A dedicated `agent` principal kind may be added later; it is not in the initial architecture. Everywhere else AI is simply a scoped, audited user ([AI](/architecture/ai/)).

Each kind that needs structured domain attributes gets a **1:1 per-kind table** linked by `principal_id`: `human`, `service`, and `node`. The base `principal` table holds identity + kind only; the per-kind tables hold the rest, including the kind's human-facing label (a human's `display_name`, a service's label, the node's name).

## Credentials

One `credential` row per authN method per principal. A principal can hold many (a human with a password + an OIDC link; a service with a rotating token). `(method, identifier)` is the lookup key.

| method | identifier | secret_hash | who uses it |
|---|---|---|---|
| `password` | `principal.id` (uuid) | argon2id of the password | humans |
| `oidc` | `iss\|sub` (issuer + subject) | null (IdP verifies) | humans |
| `token` | `sha256(token)` | null (identifier IS the verifier) | service |
| `nats` | nkey public key | null (NATS verifies the signed nonce) | nodes |

The password identifier is the `principal.id` (not the username), so a username change does not invalidate the credential. Service bearer tokens are 256-bit `crypto/rand` payloads with a human-readable prefix (`ogp_`) for secret-scanners and audit clarity; the server only ever stores `sha256(token)`. Cleartext is returned exactly once at mint time. A `node` enrolls with a per-tenant **NATS JWT/nkey** instead: the credential row stores the nkey public key, NATS verifies a signed nonce, and the JWT carries the node's subject permissions (its placement-derived `visible_set`, see [The node path](#the-node-path)).

:::caution[Open question]
OIDC delegates MFA to the IdP; whether to add a local-account TOTP path for installs not on OIDC is
undecided.
:::

## Subjects

`human`, `service`, `node`, and **`principal_group`s**. Roles attach to principals regardless of kind; the same `principal_grant` rows mean the same thing whether the principal is a person, a service, a daemon, or an AI tool acting as one.

## Group kinds

The `group` membership mechanism (static list or dynamic filter) is shared across kinds, but the kinds are kept **distinct** (not one polymorphic primitive yet, because their usage differs):

- **`component` / `system` / `location` groups** are **entity-groups**: they carry config bindings (the cascade) and serve as ABAC **scopes**.
- **`principal_group`** is a collection of principals (SCIM-synced or local): a grant **subject**, carrying no config. It groups over principals, not just humans (members can be any principal kind); in practice it is humans synced from the IdP. A grant attaches to a group the same way it attaches to a principal (the one `principal_grant` table, keyed by a group instead of a principal), and every member **inherits** it: the grant loader unions a principal's group grants with its direct grants, so an inherited grant flattens to permissions and resolves to scope identically to a direct one. Membership is static (an explicit join) and flat (no nesting) in the first cut; a group grant is bounded by the same escalation cover-check as a direct one (a granter cannot confer a tier above its own).

So `group` appears on **both sides of authZ**: `principal_group`s as subjects, entity-groups as object scopes.

:::caution[Open question]
Whether to unify the group kinds into a single polymorphic `group` primitive; revisit if their usage
converges.
:::

## Roles and the role hierarchy

:::note[One word, two models]
A **role** on this page is an IAM role: a capability set granted to a principal, in the `role` table, on the
`/roles` routes. A **[system role](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled)**
is a slot a system needs filled, in `system_role`, on the `/standards/{id}/roles` and `/systems/{name}/roles`
routes. They are separate namespaces with no crossing, which is why the storage methods for the estate side
are named `ListSystemRoles` / `SetSystemRole` / `DeleteSystemRole` rather than colliding with `ListRoles` /
`UpsertRole`.
:::

A role is a **capability set**: permissions per `(resource, action)`. Roles live in a `role` table with a uuid `id` and a globally unique, renameable `name` (the handle `owner` / `viewer` / `admin` / ... are addressed by), each carrying an **`official` boolean**:

- **`official: true`**: ship-with the binary, seeded via the boot phase. A release can patch a default permission via `ON CONFLICT DO UPDATE` on the seed.
- **`official: false`**: operator-created via the IAM API.

**No overrides**: a role name is globally unique across both kinds (the create paths refuse an `official: false` role whose name matches an `official: true` one, and the seed phase fails-safe with a loud warning if it would collide with an existing operator role). This is a deliberate divergence from the shadowable registries (where an org-scoped name may shadow an official one): role override risks lockout with no compensating use case, so a role name resolves to exactly one row.

### The five official roles

Each role carries a `display_name` and a `description` alongside its permissions (surfaced in the console's Roles view and the grant-builder tooltips). Inheritance (transitive): each role's **effective** permissions are the union of its own and all transitively-inherited roles' permissions, with wildcards and the `:read` floor resolved. `viewer` is the common floor; `operator` and `deploy` are two branches off it, and `admin` extends `operator`:

```
viewer  <-  operator  <-  admin  <-  owner
   \
    <-  deploy
```

| role | what it can do |
|---|---|
| `viewer` | Read every operator-facing resource within scope. The IAM directories (`principal`, `role`, `principal_group`) are **not** operator-facing: their reads are admin-tier (`<resource>:read:admin`), so `viewer`'s `*:read` does not reach the Users, Roles, or Groups pages ([ADR-0023](/architecture/decisions/#adr-0023-the-iam-directory-reads-principal-role-principal_group-are-admin-tier)). Secrets are likewise off the floor: `secret` is a **sensitive resource** a bare `*` does not reach, so `viewer` reads no secret directory ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)). |
| `operator` | viewer + create/update on components, interfaces, rules, config; **read, reveal, create, and update the operational secrets in scope** (`secret:read,reveal,create,update`); ack/snooze/resolve alarms. Secret **delete** stays admin-only, and an **admin-sensitive** secret (a platform credential, `admin_sensitive = true`) is invisible in the directory and a non-disclosing 404 on reveal regardless of scope, so an operator sees device secrets but never a platform key at the same scope ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)). Creating an admin-sensitive secret needs the admin tier, so an operator can mint only operational ones. |
| `deploy` | viewer + create/update on locations, systems, and components (the integrator / field-tech role, typically granted with the `subtree_excl_root` operator to build out a subtree without editing its root). No delete. Carries the same scoped `secret:read,reveal,create,update` as `operator`, for the device secrets a field tech sets up during a site build. |
| `admin` | operator + delete on managed resources + manage IAM (principals, credentials, grants, custom roles) + curate registries (`<registry>:create`) + **`platform:*`**, the install-wide authority a write at the cascade's `platform` tier needs ([below](#install-wide-authority-is-not-estate-scope)); `operator` and `deploy` hold no `platform` capability, so an all-scoped operator runs every site without being able to change the install-wide value under them. Holds `secret:>` (the tail wildcard, not the two-token `secret:*`), so it reaches the **admin-sensitive** secret tier (`secret:reveal:admin`) that a two-token wildcard cannot, and sees and reveals platform credentials. IAM management is meaningful only from an `@ all` grant (a scoped `admin @ subtree` keeps the operator powers within its subtree but gets no IAM); registry curation is a plain capability, so a custom role can carry `<registry>:create` alone for a non-admin curator. Deliberately **not** the superuser: it cannot grant a role above its own tier ([ADR-0013](/architecture/decisions/#adr-0013-a-grant-cannot-confer-capabilities-the-granter-lacks)), so it cannot make itself owner, and it cannot delete `official` roles. |
| `owner` | The break-glass superuser (`>`, the tail wildcard, covering every capability at every tier, including admin-sensitive ones and future resources). The unkillable role: at least one active `owner@all` grant must exist at all times (enforced by DB trigger), and an owner account cannot be impersonated. The bootstrap creates the first owner. |

The console **Roles view** (`GET /roles`, gated `role:read:admin`) lists these read-only with each role's display name, description, inheritance, and **effective permissions**, so an operator sees exactly what a role grants before assigning it. The role blade shows permissions as a **net** view: the response carries the **permission universe** (every capability the route surface enforces, see [the permission universe](#the-permission-universe-published-per-route)) plus, per role, the **held** subset (resolved server-side by the same `rbac.Set.Allows` matcher as the effective set, so wildcards, the `:read` floor, and the `>` tail are honoured). The blade renders the universe one-per-line, lexicographically, with a `Held / Missing / All` toggle, so an operator reads not only what a role holds but what it is missing. Custom-role editing is a later slice.

### Custom roles

Operators create `official: false` roles via the IAM API with a chosen permission set, optionally inheriting from `viewer` (or any other role). Inheritance rules:

- An `official: true` role may inherit only from other `official: true` roles (enforced at seed time).
- An `official: false` role may inherit from any role.

Because of the no-override rule, `inherits: [viewer]` is unambiguous (every id resolves to exactly one role).

### Permission format

Permissions are **topic patterns**, matched like [NATS](/architecture/messaging/) subjects (which the node path already uses, so the whole stack shares one wildcard convention): a colon-delimited token path where a **literal** matches itself, **`*` matches exactly one token**, and **`>` matches one or more tokens and must be last** ([ADR-0015](/architecture/decisions/#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)). One entry per resource per role; the action segment may be comma-separated (a shorthand that expands to one permission each).

```
component:read                <- one permission
component:create,update       <- expands to component:create and component:update
alarm:ack,snooze,resolve      <- domain verbs alongside CRUD
datapoint_type:create         <- a registry curator capability (tag/unit/event_type/severity_level/source likewise)
principal:*                   <- any single action on this resource (a two-token pattern)
audit:read:admin              <- an admin-sensitive permission (three tokens)
>                             <- everything, at every tier (the owner superuser)
```

A normal permission is `resource:action` (two tokens); an **admin-sensitive** one carries a third `admin` token (`audit:read:admin`). Because `*` matches exactly one token, a two-token pattern like `*:read` (viewer) or `*:*` or `principal:*` **structurally cannot** match a three-token `:admin` permission: admin-sensitivity is a **deeper token**, not a special case in the matcher. The IAM directory reads (`principal:read:admin`, `role:read:admin`, `principal_group:read:admin`) use this to keep the Users, Roles, and Groups pages off the `viewer` floor, alongside `audit:read:admin` and `principal:purge:admin`; `admin` carries each explicitly, since its `principal:*` (two tokens) cannot reach them. The whole-estate superuser is `>` (owner); `<resource>:>` grants everything under one resource including its admin tier. Actions are HTTP-aligned: `read` (GET), `create` (POST), `update` (PATCH/PUT), `delete` (DELETE), plus resource-specific verbs (`ack`, `snooze`, `resolve` for alarms; future kinds add their own). The aggregate `write` does not exist as an alias.

There is one further carve-out: a small **sensitive-resource set** that a bare single-token `*` does not reach, in **both** places `*` can grant (the direct topic match and the `:read` floor). It exists for a resource that is not `:admin`-tier wholesale (an operator legitimately holds a two-token grant on it) but that a bare wildcard must not sweep up. The set is `{secret, settings, platform}`, one reason each: an operator holds a literal `secret:read` and reads its scoped device secrets while a `*:read`-only `viewer` reads none; `settings` is install configuration whose admin read-with-provenance is not part of the viewer floor (an ordinary user sees only the client-visible namespaces, through the authn-only `/settings/me`); and `platform` is not a resource anyone reads at all but install-wide **authority** ([below](#install-wide-authority-is-not-estate-scope)), which full-estate **reach** must not confer. A literal (`secret:read`), a resource wildcard (`secret:*`), and owner's `>` still name a sensitive resource; only the **bare** `*` is turned away ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)). This is distinct from, and composes with, the `:admin` tier: `secret` uses **both**, a per-secret `admin_sensitive` flag flipping an individual platform credential to the `:admin` tier (see [config and credentials](/architecture/variables/)), while the set keeps the whole resource off the viewer floor. The IAM directories are **not** in the set (they have no legitimate sub-admin reader, so the `:admin` tier alone suffices).

Inheritance composes permissions **by union**:

```
parent: component:create,update
child:  component:delete
child effective:  component:create, component:update, component:delete
```

There are no negative permissions. To narrow a parent's capability set, define a fresh role rather than inherit. The escalation guard (`rbac.Set.Covers`) uses **pattern subsumption**: a broader pattern covers a narrower one, `>` covers everything, and no partial wildcard covers `>` (so an admin, holding no `>`, can neither impersonate nor grant an owner).

### The permission universe, published per route

Every capability-gated route registers through one helper, `gated(op, tokens...)`, which sets the `authn` + `require` middleware, **stamps the operation** with the `x-omniglass-permission` OpenAPI extension, and records the permission in an in-process registry. Because the stamp lands on the Huma operation, the required permission for each request is **published in the generated `api/openapi.json`** (and the YAML): the authz contract is part of the API-first spec, machine-readable by the CLI, the typed client, the docs, and any external reader, not buried in Go middleware. The **permission universe** is the sorted, deduped set of every stamp: exactly the capabilities the API enforces, derived from the routes with no hand-kept catalog to drift. The Roles view reports it (above) as the denominator for the net held-vs-missing view.

A permission a role **grants** but the universe does **not** contain is a capability that is not yet routed (enforces nothing). These are legitimate but ahead of their subsystems (for example `alarm:*`, `interface:*`, `task:*` before the collection and event engines expose HTTP surfaces). They show as held-nothing on a role blade, which is the honest signal, and a **drift test** requires each such seed grant to sit in an explicit `aheadOfRoutes` allow-list until its route lands.

## Authorization: grants = role x scope

A principal holds grants in `principal_grant`. Each grant is a `(role, scope_kind, scope_id)` triple. A principal can hold many grants; they are **additive**:

```
canDo(P, action, E)  iff  exists grant g in grants(P) such that
                            action in perms(g.role)
                            AND E in expand(g.scope_kind, g.scope_id)
```

**Action and scope bind per grant, not globally.** The `action` and the `E`-membership test are satisfied by the **same** grant `g`. It is **not** sufficient that the action appears in *some* grant and the entity in *some other* grant: a principal with `operator @ group-A` (which carries `alarm:ack`) and `viewer @ all` (read-only) can ack only alarms whose component falls in `group-A`, never estate-wide, because no single grant pairs `ack` with an all-scope. Flattening permissions into one global set and entities into one global visible set is **not** equivalent to `canDo` and over-permits; the enforcement layers below preserve the per-grant binding.

So the same role applied at different scopes composes naturally; mixing roles (e.g., `operator @ HQ` + `viewer @ all` for a site lead who needs read-only visibility outside their primary site) is the intended pattern. Grants from `principal_group` memberships compose the same way.

### Scopes

| scope_kind | scope_id | expansion |
|---|---|---|
| `all` | null | every entity in the database |
| `location` | location id | subtree(L): L + its systems + their components + descendants |
| `system` | system id | subtree(S): S + its components + descendants |
| `component` | component id | exactly { C } |
| `group` | group id | members(G) at resolution time (dynamic groups re-resolve) |

`expand` realizes a scope to a **bound id set** the gateway injects as a parameterized `owner IN (...)` predicate (or a closure-table join for deep trees), never string-built. The structural-tree walk carries a cycle guard, and the set is **fleet-size-bounded** (entities), so it stays an indexed membership filter.

`scope_kind` is enumerated (`all`, `location`, `system`, `component`, `group`); adding a new kind requires a schema change (CHECK constraint) and a new case in the gateway's `expand` function. `scope_id` is operator data.

:::caution[Open question]
Whether a scope may mix include and exclude (e.g. "all except group X").
:::

### Install-wide authority is not estate scope

The [cascade](/architecture/cascade/)'s least-specific tier is **`platform`**: the value an admin set for the
**whole install**, above every location tree top. A write there needs **two** permissions, the resource's own
and **`platform:<action>`**, checked together ([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)):

```
secret:create   + platform:create    <- seal a secret at the platform tier
variable:update + platform:update    <- change the install-wide value of a variable
tag:update      + platform:update    <- POST /tags/{name}:setPlatform
settings:update + platform:update    <- every settings write, which is install-wide by definition
```

The two answer different questions. A **scope** says *how much of the estate* a grant reaches; `platform:<action>`
says whether the principal may change *the value under all of it*. Collapsing them would mean any all-scope grant
silently carried install-wide authority, so a senior operator trusted with every site could also change the
install-wide value every one of those sites inherits. `platform:*` is seeded to **`admin`** (and reaches `owner`
through `>`); `operator` and `deploy` hold no `platform` write, and **nothing implies one**. That last part is
mechanical, not a convention: `platform` is in the [sensitive-resource set](#permission-format), so a bare
single-token `*` never names it, in either place `*` can grant. Only a literal (`platform:update`), a
resource wildcard (`platform:*`), or owner's `>` does, which is why a custom role carrying `*:update` holds every
estate write and still no install-wide authority. The console mirrors the same set, so it offers the tier controls
(the Platform scope on the secret and variable create forms, Edit and Delete on a tier row, the settings write
affordances) only to a caller holding both halves, and names the missing capability rather than letting the
operator earn a 403.

The tier gate is **published per route** like every primary gate: a route that can write at the tier carries an
`x-omniglass-platform-permission` extension beside its `x-omniglass-permission` stamp, and both land in the
route-derived [permission universe](#the-permission-universe-published-per-route). Where the request already
names the tier the handler refuses up front (403); where only the stored row knows its tier (an update or delete
by id) the resolved capability is passed into the Storage Gateway beside the ABAC scope, so a caller who may read
the row but not act at the tier gets 403 and one who cannot read it gets a non-disclosing 404, the same split
every other write uses.

## Visibility cascades down the structural tree

A scope of entity E includes E **and everything structurally beneath it** (a location -> its systems -> their components -> their datapoints and alarms). The visible set is **parameterized by action**: `visible_set(P, action)` = the union, over **only the grants whose role carries `action`**, of each scope entity plus its descendants. There is no single global visible set. **`:read` is an implicit floor on every grant**: holding any grant on an entity confers `read` on it, so `visible_set(P, read)` is always the widest set and `visible_set(P, action)` is always a subset of it. The floor is realized as a **capability injection at role-index build** (next): every `<resource>:<action>` permission implies `<resource>:read`, so the implied reads are present in the fast-reject union, in `canDo`'s `perms`, and in `/auth/me.permissions`, not only in the scope layer. A verb-only role (`alarm:ack` without `alarm:read`, no `viewer` inheritance) is therefore **not** hard-403'd on the read. The asymmetry runs one way only: a principal can **read** an entity it cannot **act** on (in `visible_set(P, read)` but outside `visible_set(P, ack)`, via a read-only grant), but never the reverse. So there is no "actionable but not readable" case, and the status split below stays three-way. Dynamic-group scopes recompute as membership changes. Each per-action set is bounded by **fleet size (entities)**, not data volume.

### Scope operators (how a grant's root matches the tree)

A grant carries a **`scope_op`** that says how its root matches the tree, a small operator instead of a pile
of boolean modifiers. It lives **on the grant**, not as a new scope kind, so it composes with the
additive-grant model and confines the change to one predicate. It is moot for the `all` scope.

| operator | glyph | in scope | for |
| --- | --- | --- | --- |
| `subtree` (default) | ≥ | the root **and** everything beneath it | every action |
| `subtree_excl_root` | > | the root's descendants; **not** the root itself | update / delete (read and create keep the root) |
| `self` | = | **exactly** the root row, no descendants | read / update / delete (**not** create: no children) |

`subtree` is the ordinary case. `subtree_excl_root` is the integrator / deploy grant: `deploy @
location:room-42 (>)` lets a field tech add and edit the systems and components inside room-42 without being
able to rename or delete room-42. It narrows only the **modify** actions to the descendants; read and
create-placement still include the root so the holder can see the boundary of its scope and place children
under it. A `PATCH` on the root is then the readable-but-out-of-write-scope **403** (not a 404: the target is
readable), while a `POST` under the root and a `PATCH` on a descendant succeed. `self` is the tightest grant, a
leaf-lock on one node: exactly its own row for read, update, and delete, never a descendant, and (unlike the
two subtree operators) **not** create-placement, so it grants no authority to grow the tree under the node. So
`operator @ location:room-42 (=)` sees and edits only room-42, cannot add a child under it (a `POST` under it
is a **403**), and the list returns only room-42.

Operators combine by union across grants, resolved per action: an inclusive `subtree` grant on a root wins
over an excluding one, and a `self` grant re-admits a root that a `subtree_excl_root` grant stripped (the
subtree walk still skips the root; the self predicate matches its row). The operator is part of a grant's
identity: the same role at the same root with a different operator is a **distinct** grant, so changing an
operator is a revoke plus a grant.

## The owner invariant

At least one active `owner @ all` grant must exist at all times. Enforced as a deferrable constraint trigger in Postgres (fires at `COMMIT`, so the swap-owners pattern works in one transaction):

```
BEGIN;
  INSERT INTO principal_grant (... role='owner', scope_kind='all' ...);  -- new owner
  DELETE FROM principal_grant WHERE principal_id=<old> AND role='owner';  -- old
COMMIT;  -- trigger fires here, sees the new grant, passes.
```

Attempting to remove the last owner (by grant delete, principal delete, principal disable, or role change) raises a check-violation. The Gateway translates this into a 400 with a clear remediation message.

### Grants cannot exceed the granter

Creating a grant is refused (403) when the granted role's capabilities are not **covered** by the granter's own **all-scope** capabilities (`rbac.Set.Covers`, the same primitive as the impersonation escalation guard). So no caller can promote anyone, including itself, to a tier above its own: an **admin cannot grant `owner`** (`>`), because admin is an enumerated role whose patterns do not subsume the superuser tail, and it therefore cannot self-promote to the superuser tier. Only the caller's **all-scope** grants count, so a capability held through a narrower grant cannot be conferred estate-wide. This makes the owner tier a real capability firewall: an admin is deliberately bounded (the top management role, not the superuser), and a self-grant is not a path from admin to owner. The same rule will apply to role editing when it lands (you cannot edit a role above your own tier).

## Impersonation (view-as and act-as)

An owner or all-scope admin holding `principal:impersonate` can temporarily see and act through another
principal, for troubleshooting. Two modes: **view-as** resolves reads under the target's `visible_set` and
refuses every write (read-only), while **act-as** is full, and its mutations are attributed to **both** the
impersonated principal and the real admin. `POST /principals/{id}:impersonate` mints a bounded (default 30
minutes, revocable) bearer token stored as an `impersonation_session`, a table deliberately distinct from
`credential`: a credential authenticates a principal **as itself**, a session authenticates one principal
**as another on someone's behalf**, a materially different fact with its own expiry, revoke, and "who is
impersonating whom" listing. The client sends that token, and `authn` resolves it on a bearer miss to the
**target** principal, tagging the request with the real actor and the mode; `POST /auth/me:stopImpersonation`
revokes it.

Two guarantees make it safe, over a hard floor. **Owner protection**: a principal holding `owner @ all` is
un-impersonatable by **anyone**, including another owner, in either mode; an owner is the highest-trust
account, so impersonating one is a full-takeover vector, removed entirely rather than left to the cover
arithmetic. The **escalation guard**: a caller may impersonate a (non-owner) target only when the
caller's capabilities **cover** the target's (`rbac.Set.Covers`), so impersonation can never confer a
capability the caller lacks (a lesser admin cannot impersonate an owner, and owner protection makes that
absolute). Scope is where the modes differ:
view-as is cross-scope (read-only grants no write authority), but **act-as** additionally requires the
caller's **all-scope grants alone** to cover the target, since an impersonated request resolves its scope from
the target: a capability the caller holds only through a narrower grant does not count. Without it a
split-grant admin (all-scope user management, campus-scoped infra) could act-as a different campus's admin and
gain write there. The rule is resource-agnostic, so it also closes escalation through non-tree writes
(`principal_grant`, `role`) whose scoped grants resolve to an empty effective scope: a user-admin who cannot
create a single grant directly cannot launder all-scope grant authority by acting-as a grant admin either. And **accountability**: every audited mutation taken while
impersonating records `real_actor_principal_id` alongside the impersonated `actor_principal_id`, so the true
actor is never lost (the self-service `/auth/me` profile and password edits audit too). Self-impersonation is refused, nesting is refused, and
disabling either the target or the real admin kills the session on its next request (the same per-request
`active` re-read that makes disable hard revocation).

## Enforcement: where each check lives

There is **no RLS and no direct database access** (no PostgREST). The **Storage Gateway is the only door to the database** and the API is its only caller, so authz lives entirely in the app. A targeted mutation passes three checkpoints in order: the **capability fast-reject** at the route, the **`canDo` decision** in the handler, and the **per-action scope plus audit** injected by the gateway. Each is one code seam:

```d2
direction: down
classes: {
  node: { style.border-radius: 8 }
  group: { style.border-radius: 8 }
}
client: "Client: SPA / CLI / MCP" { class: node }
api: "API process (one binary)" {
  class: group
  mw: "Route middleware\nrbac.Require('alarm:ack')" { class: node }
  mwq: "action in\nANY grant?" { class: node; shape: diamond }
  e403a: "403 capability missing" { class: node }
  handler: "Handler" { class: node }
  hq: "canDo(P, ack, X) ?" { class: node; shape: diamond }
  e403b: "403 cannot act on target" { class: node }
  e404: "404 non-disclosing" { class: node }
  mw -> mwq
  mwq -> e403a: "no"
  mwq -> handler: "yes: fast-reject passed"
  handler -> hq
  hq -> e403b: "readable, not ack-scope"
  hq -> e404: "out of read-scope"
}
gwbox: "Storage Gateway: the only DB door" {
  class: group
  gw: "inject visible_set(P, ack)\nplus audit_log in one txn" { class: node }
  db: "Postgres" { class: node; shape: cylinder }
  ok: "200 plus action row" { class: node }
  gw -> db: "parameterized predicate"
  db -> ok: "1 row changed"
}
kv: "NATS KV cache\ngrants plus role index\nCDC-invalidated" { class: node; shape: cylinder }
client -> api.mw: "POST /alarms/X:ack"
api.hq -> gwbox.gw: "yes"
gwbox.db -> api.e403b: "0 rows: backstop fires"
kv -- api.handler: "composed per request" { style.stroke-dash: 4 }
kv -- gwbox.gw { style.stroke-dash: 4 }
```

The capability check is **necessary not sufficient** (it only rejects), the `canDo` check is the **authoritative decision**, and the gateway predicate is the **enforce-by-construction backstop**: handler and gateway return the same status for the same input, so a forgotten handler check cannot leak a write. The detail of each:

- **Capability (RBAC) in the API middleware is a FAST-REJECT, never an authorization.** It answers one necessary-but-not-sufficient question: does the action appear in **any** of the principal's grants? If not, 403 before the gateway is ever touched. Answered from an in-process cache (the flattened union of permissions across all grants). It never grants access: passing the fast-reject only means "not categorically forbidden", scope still decides. Routes declare their required permission with `rbac.Require("component:create")`.
- **Scope (ABAC) in the Storage Gateway is per-action.** Every query carries `visible_set(P, action)` for the **specific action** being performed (read for a list/get, ack for an `:ack`, command for a `:command`), and the gateway filters rows by their exclusive-arc owner against that action-specific set (the owning `component`/`system`/`location`). A read uses `visible_set(P, read)`; a write uses `visible_set(P, write-action)`, the union of scopes of **only** the grants whose role carries that write action, never the read set and never a global union. This is the enforce-by-construction backstop: an `:ack` whose target lies outside `visible_set(P, ack)` matches **0 rows** even if the handler forgot its up-front check. A gateway write whose action-scoped predicate affects 0 rows is **never a silent success**: the gateway reports the miss to the handler, which returns 404 (target also outside `visible_set(P, read)`, non-disclosing) or 403 (target readable but outside the action scope), matching the up-front `canDo` decision for the same input. A silent 200/no-op is a correctness bug and is forbidden. Each per-action set is bounded by **fleet size (entities), not data volume**, so it stays an indexed membership filter even on the firehose; and because it is an owner filter in app code, not a DB policy, it works identically on Postgres, the columnar tier, or object storage.
- The gateway has three query **modes**: **scoped** (an API request carrying a principal's visible set), **node** (a node-driven write confined to the node's placement-derived `visible_set`, the owners of the tasks assigned to it from its NATS subject grants), and **system** (trusted internal work: the CDC publisher, the datapoint persistence sink, reconcile / migrate / seed, all-visibility). Node mode sits between scoped and system: a node is trusted to write platform internals on behalf of itself, but only for the owners it actually covers, so a compromised node cannot write arbitrary owners intra-tenant. System mode is an explicit, audited choice, never the default. There is no fourth path: any storage caller is one of these three.
- **Targeted mutation on a known id evaluates `canDo` up front.** A custom method against a specific id (`POST /alarms/X:ack`) evaluates `canDo(P, action, X)` in the handler **before** dispatch, so the decision is clean and explicit, with the gateway per-action predicate as the backstop for a forgotten check. The status split is fixed and three-way, not binary: (a) action in **no** grant -> 403 at the middleware fast-reject (capability missing entirely); (b) target in `visible_set(P, read)` but **outside** `visible_set(P, action)` -> **403** (the principal can read X but cannot perform this action on this target); this 403 leaks no existence, because the caller can already read X. (c) target **outside** `visible_set(P, read)` -> **404**, non-disclosing, exactly as an out-of-scope read. The up-front check and the gateway backstop return the **same** status for the same input.
- **Scope is structural, not per-handler**: the principal's scope is a required input to the gateway's query layer, so no code path can query unscoped by accident. With no RLS backstop for in-database scope the gateway is the sole guarantor, so "forgot to filter" must be impossible by construction, not by discipline.
- **Coverage scales with the surface, by test, not by discipline.** Three conformance tests keep authorization honest as entities and routes are added. An **authz conformance matrix** runs the full assertion set (capability 403, the over-permit scope 403 on a readable-but-out-of-write-scope target, the non-disclosing 404, in-scope success, the read/act asymmetry) against **every** scoped entity from a registry: a new scoped entity is one registry line and inherits the whole matrix. A **route-gating guard** enumerates the generated OpenAPI and drives each operation with an authenticated zero-permission principal, asserting a 403 for every route outside a short, justified allow-list (the public probe, and the authn-only self-service `/auth/me` routes); a route that forgets its capability gate fails the build. A **published-gate guard** is its spec-side companion: it asserts every route outside that same allow-list carries an `x-omniglass-permission` stamp (and every allow-listed route carries none), so "gated" and "published" are the same set and the OpenAPI is a faithful map of the authz contract. So the capability and scope cores are written once and proven for the whole surface, rather than re-tested per feature.

**Worked example (per-grant binding denies estate-wide ack).** Principal P holds two grants: `operator @ group-A` (role carries `alarm:ack`) and `viewer @ all` (read-only). Alarm X is owned by a component in **group-B**. P calls `POST /alarms/X:ack`:

1. **Middleware fast-reject**: `alarm:ack` appears in *a* grant (the `operator @ group-A` one), so it passes. (This is why fast-reject is necessary-not-sufficient: it cannot see that the ack-carrying grant does not cover X.)
2. **Up-front `canDo(P, ack, X)`**: the only grant whose role carries `ack` is `operator @ group-A`; X is not in `expand(group-A)`. `viewer @ all` carries `ack` = no. So `canDo` = **false**.
3. **Status**: X is in `visible_set(P, read)` (via `viewer @ all`) but outside `visible_set(P, ack)`. Branch (b): **403**, "cannot ack this alarm", not a 404 (P can already `GET /alarms/X`, so non-disclosure does not apply).
4. **Backstop**: had the handler skipped step 2, the gateway's `:ack` write carries `visible_set(P, ack)`, X is outside it, the UPDATE matches 0 rows, and the gateway returns the same 403, never a silent success.

The flattened-set model would have wrongly allowed this: `ack` is "in the permission set" and X is "in the global visible set", so the per-grant binding is exactly what stops estate-wide ack.
- **Non-entity resources** have no entity `E`, so `canDo` cannot scope by owner. Three governance classes:
  - **IAM subjects** (`principal`, `role`, `principal_grant`, and a principal's **login credential** create/delete): the action must appear in a grant whose `scope_kind` is `all`. A scoped grant confers **no** IAM capability, so `role:create` carried by an `operator @ HQ` grant does not let you create roles. Typically `owner @ all` / `admin @ all`. (Device secrets are a different resource: a **credential variable** is entity-scoped, so its `secret:read` plaintext decrypt and its rotation are ordinary scoped actions against the credential's owner, [config and credentials](/architecture/variables/).)
  - **Data registries** (`datapoint_type`, `tag`, `unit`, `event_type`, `severity_level`, source): governed by a distinct **`<registry>:create` curator capability** (`datapoint_type:create`, `tag:create`, `unit:create`, `event_type:create`, `severity_level:create`, `source:create`). A registry entry has no owner entity, so the grant's `scope_kind` is irrelevant: the check is simply whether the principal holds the capability. Granting it to a curator role lets a principal mint registry entries **without** IAM admin; a minted entry carries its own `scope` (an org-scoped entry shadows an official one, the [namespace-shadow pattern](/architecture/datapoints/#key-scope-template-org-official)), and `official`-scoped entries are reserved to `owner` and the boot seed.
  - **Type registries** (`location_type`, `secret_type`): governed by a single shared **`type`** resource carrying the full verb set (`type:read`, `type:create`, `type:update`, `type:delete`), unlike a data registry's create-only curator capability. `type:read` needs no separate grant, since `viewer`'s `*:read` floor already covers it; `type:create,update,delete` is granted to `admin`. The **`location_type` property contract** (`/location-types/{id}/properties`) hangs off the same resource, so declaring a property on a type is `type:update` and withdrawing one is `type:delete`. As with data registries, `scope_kind` is irrelevant and the check is simply whether the principal holds the capability, since a type row has no owner entity to scope by. Unlike a data registry entry, a type row carries no namespace-shadow `scope`: an `official` (seed-owned) row is read-only (create/update/delete 422), an operator-created row is `official: false` and freely editable, and deleting a row still referenced by a location is refused (409), first by a Gateway pre-count for the friendly error and, as a backstop, by the parent foreign key. The `official` read-only treatment is reserved for the **canonical catalogs**; the **shipped location types seed as `official: false`**, so they are operator-owned and editable ([the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)).
  - **Catalog entities** (`vendor`, `driver`, `capability`, `product`, **`standard`**): each takes its **own** resource with the full verb set (`product:read` / `:create` / `:update` / `:delete`, `standard:read` / ..., and so on) rather than the shared `type:*`, because each declares more than a label. A **component** carries no type: its shape comes from its `product`, whose delete guard is the `component.product_id` restrict FK. A **system** likewise carries no type: it **conforms** to a `standard`, which is why `standard` graduated out of the `type` registry when it gained a property contract ([core entities](/architecture/core-entities/), [ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model)). Each catalog's property contract rides its owner's permission (`product:update` declares on a product, `standard:update` on a standard).

  The fast-reject still only rejects; for these resources the authorization is the grant-class check (an `all`-scoped grant for IAM, the `<registry>:create` / `type:<action>` / `<catalog>:<action>` capability for registries and catalogs), the one place the decision is capability-shaped because there is no entity to scope.

Both layers operate **within one database**. Tenant isolation is **per-deployment**: a tenant is one database plus one **NATS account** plus one deployment, so per-database isolation (storage) and per-account isolation (messaging) are the same boundary. There is no `tenant_id` column anywhere, so the cross-tenant boundary is the database / account boundary itself, not a row predicate. Intra-database scope (above) is the only app-enforced layer; there is no RLS backstop.

:::caution[Open question]
Whether to add a **third authorization lever**: a declarative **tenant-level policy** layer, evaluated at
the **highest priority** above RBAC and ABAC, expressing **negative guardrails** an admin declares
centrally, the things that must **never** happen. A grant plus scope might permit `system:delete`, yet a
tenant policy ("no member of the `integrator` group may ever delete a system") **denies** it, and the
deny wins. This is where negative authorization would live, keeping [roles](#roles-and-the-role-hierarchy)
additive and positive (a role still carries no negative permissions). Open: whether to add it at all, the
policy shape (deny rules over resource + action + subject / scope conditions), the evaluation order, and
whether it is deny-only or can also force-allow.
:::

## Caching strategy

The hot path must not hit the DB for RBAC. Three layers, in-process, no persisted "effective permissions" projection (which would invite the stale-join class of cache-coherence bug; the grant and role caches below still carry a bounded staleness, the contract for which is stated at the end):

1. **Role index**: at boot, the `role` table is loaded into a Go map with `inherits` resolved transitively, wildcards expanded, and the **`:read` floor injected** (each `<resource>:<action>` adds the implied `<resource>:read`, so the floor is in the flattened union the fast-reject reads, not only the scope layer). Refreshed on a NATS KV watch keyed on `role` changes.
2. **Principal cache**: at session establish (or first token-auth), the principal's **grants** and the `role -> perms` index are cached by `principal_id`; the flattened `Set[resource:action]` (used only for the fast-reject and `/auth/me`) is derived from them. Invalidated on a NATS KV watch keyed on `principal_grant`, `principal`, or `role` changes. **Group membership is resolved live in-query** (no materialized member-set cache), so a dynamic group's expansion is always current.
3. **Per-request**: the per-action authorization is **composed at request time** from the cached grants + `role -> perms`. The middleware does an O(1) Set-membership fast-reject on the flattened permissions; the gateway builds `visible_set(P, action)` for the **specific action** by unioning the scopes of only the grants whose role carries it. The flattened set never authorizes; it only fast-rejects. Both O(1)-with-a-prefactor in the common case.

The DB is the source of truth; caches are derived views with explicit invalidation events. The principal/permission cache, config, and distributed locks live in **NATS KV** (not Postgres `LISTEN/NOTIFY`): a committed change to `role` / `principal` / `principal_grant` reaches NATS through the leader-elected CDC publisher, which updates the KV keys those watches observe. The same KV contract holds whether the design runs single-binary (embedded NATS) or against an external NATS cluster at scale.

**Staleness contract.** Both the handler `canDo` and the gateway predicate read the **same** cached grants, so the gateway backstops a *forgotten* check, not a *stale* one: a revoked-but-not-yet-invalidated grant authorizes at both layers. The grant cache therefore carries a **bounded max-staleness**, a TTL floor independent of CDC invalidation, so a CDC-publisher outage or failover cannot extend the revoke-lag window unbounded. For **high-sensitivity mutations** (IAM changes and deletes of IAM objects) the gateway **re-resolves grants in the transaction** against source-of-truth, trading a round trip for zero revoke-lag; that round trip is off the read and firehose **hot path** (which never hits the DB for RBAC). Other control-plane mutations (`:ack`, `:command`, a config `PATCH`) take the cached path and so accept a **bounded revoke-lag** (the TTL floor above): documented and bounded, not closed. An open SSE session **re-checks on every grant-cache invalidation** for its principal (next section's relay) and closes if `:read` is lost. The freshness asymmetry is deliberate: grant membership (the **subject** side) is cached and is the binding staleness constraint, while group membership (the **object** side) is resolved live in-query, so it can only tighten, never loosen, a stale grant.

## The /auth/me contract

The web app (and any CLI client) gets the principal + their effective permissions in one call:

```json
GET /api/v1/auth/me
{
  "principal": { "id": "...", "kind": "human" },
  "human":     { "username": "jordan", "email": "jordan@example.com", "display_name": "Jordan Rivera", ... },
  "permissions": [
    "component:read", "component:create", "component:update",
    "alarm:read", "alarm:ack", "alarm:snooze", "alarm:resolve",
    ...
  ],
  "grants": [
    { "role": "operator", "scope_kind": "location", "scope_id": "HQ" },
    { "role": "viewer",   "scope_kind": "all",      "scope_id": null }
  ]
}
```

The `/auth/me` family is also where a principal manages **its own** identity: `PATCH /api/v1/auth/me` edits the caller's own `display_name` (email is an administrator-set field, not self-editable), and `POST /api/v1/auth/me:changePassword` (an AIP `:verb` custom method) verifies the current password and installs a new one. Both are **authn-only and self-scoped**: they resolve the target from the session, never a path id, so they need no capability and join the route-gating allow-list next to the `GET`. Acting on **another** principal (create, disable, reset, regrant) is the admin surface and does carry capabilities. The same self-scoped family is where a principal manages **its own sessions**: `GET /api/v1/auth/me/sessions` lists the caller's **live** bearer credentials (a web login is a `session`, a CLI/API credential a `token`, told apart by the stored `credential.purpose` since both now carry an expiry; the request's own credential is flagged `current`, and an expired row is omitted the same way `AuthenticateBearer` refuses it) returning only non-secret metadata (the `sha256(token)` is compared in-query to mark `current` and never leaves the database), and `POST /api/v1/auth/me/sessions/{id}:revoke` deletes one of them, **bounded to the caller's own principal** so a credential id belonging to another principal is a non-disclosing 404 rather than a cross-principal revoke; revoking the current credential is permitted and signs that session out. A **bulk** self counterpart, `POST /api/v1/auth/me/sessions:revokeAll` with a `{ purpose }` body (`session` or `token`), ends all of the caller's own sessions or all its tokens at once, **always keeping the credential that made the request** (so a user is never signed out of the one they are on), and returns the count; it reuses `RevokeBearersByPurposeExcept` (the current session hash in `keep`), the same primitive the self-service change-password force-logout uses. A user can also **mint its own** API token with `POST /api/v1/auth/me/tokens` (a **required** description and an optional `ttl_days`, default 90, hard-capped at 365 by validation), which returns the secret **once**. All are authn-only and self-scoped. Every bearer credential now carries **identifying metadata**: a token's **description** (required on a token, empty on an auto-created session), the **user-agent** and **client-ip** that created it (captured by a small middleware before Huma, so both the public login and the token mint see them), and a **`last_used_at`** bumped on authentication (throttled to at most once a minute so a burst of requests is not a burst of writes), so the list shows a device label, the creating address, and a last-active time. Location from the IP (a GeoIP lookup) is **deferred**; v1 records the raw IP and User-Agent only. The console splits the list into a **Sessions** section (web logins) and an **API tokens** section (CLI/API credentials), each rendering the same list primitive, with a **Revoke all** on each section header and a **Create token** action on the tokens section.

The **admin** counterpart lets an administrator see and end **another** principal's sessions, so a lost laptop or a leaked API token can be cut off without resetting the account. `GET /api/v1/principals/{id}/sessions` lists the target's active bearer credentials in the same non-secret shape (`session` vs `token`, the `ogp_` locator, created and expiry), with `current` **always false** (there is no "this request's own session" when viewing someone else: the list passes a nil `currentHash`, so no row is ever flagged). `POST /api/v1/principals/{id}/sessions/{sid}:revoke` ends one (204). Both are gated by the new **`principal:revoke-session`** capability, a normal two-token permission held by `admin` and `owner` through their `principal:*` / `>` wildcards, kept separable so a future help-desk role can be granted only it. The revoke reuses the same principal-scoped delete as the self-service one, so a `sid` that is not the target's matches nothing and is a non-disclosing 404, never a cross-principal revoke. It sits behind the same **takeover guard** as impersonation and the password reset (fetch the target, then check): an **owner's sessions cannot be revoked by anyone** (a 403, so a lesser admin cannot sign an owner out from under them), nor can a caller revoke a principal whose capabilities exceed its own. The revoke is **audited with the acting admin as the actor** (the real actor rides context when impersonating), recorded as an auth-domain event (`verb = revoke_session`). The list itself is read-only and carries no takeover guard, so an admin can *see* an owner's sessions even where it cannot end them. A **bulk** counterpart, `POST /api/v1/principals/{id}/sessions:revokeAll` with a `{ purpose }` body (`session` or `token`), ends **all** of one kind at once (a purpose-filtered `RevokeBearersByPurpose`, so revoking sessions never touches tokens) and returns the count; it carries the same `principal:revoke-session` gate, the same takeover guard, and the same audit, so an owner's credentials cannot be bulk-revoked by a lesser admin either.

`permissions` is flat and wildcard-expanded, ready for O(1) `useCan(...)` checks in the web app. It is a **fast-reject / UI hint only**, the union over all grants: it answers "could this principal ever do X anywhere", never "can it do X to **this** entity". List visibility likewise (a row in `GET /alarms` is read-scoped) does **not** imply per-action authority on that row. Per-row action affordances (the ack/snooze button on a specific alarm) must be computed against `visible_set(P, action)` for that target, which the `grants` array drives: `grants` is the source for advanced UI logic (scope chips, deciding per-row actionability, explaining why a button is or is not shown). The server is the only authority regardless; the flat list and the list view are hints, the scoped gateway decides.

## Profile pictures

A human principal can carry a **profile picture**, managed on two lanes that mirror the rest of the identity
surface. **Self**: any signed-in user sets or removes their own avatar (`POST /auth/me:setAvatar` /
`:removeAvatar`), authn-only and self-scoped, needing no capability, on the same ungated lane as the profile
edit and change-password. **Admin**: `principal:set-avatar` (an all-scope capability, held by `admin` through
`principal:*` and `owner` through `>`) sets or removes **any** principal's avatar
(`POST /principals/{id}:setAvatar` / `:removeAvatar`), audited with the administrator as the actor. An avatar
is not a capability, so the admin lane carries no takeover guard (unlike a password reset). The one-line
catalog entry: `principal:set-avatar` is *set or remove any principal's profile picture; self-management
needs no capability*.

The upload is **normalized server-side and authoritatively** by the pure `avatar.Normalize` primitive, so the
client cannot bypass it: it accepts JPEG, PNG, or WebP (GIF and everything else is rejected), refuses a
payload over 8 MiB or any source dimension over 8000px (two decompression-bomb guards, one on the input bytes
and one on the decoded pixels), center-crops to the largest centered square, resizes to 256x256, and
re-encodes as JPEG at quality 82. A bad or oversize image is a **422**. The one normalized size is stored
**base64 on the human row** (`avatar`, alongside `avatar_updated_at`); the bytes are **never** loaded on the
`loadPrincipal` hot path, which selects only `avatar is not null` (a bool), so the read models carry a cheap
`has_avatar` flag and the console knows whether to render an image or fall back to initials without paying for
the payload per row.

The **read side is a JSON endpoint** (`GET /principals/{id}/avatar` gated `principal:read:admin`,
`GET /auth/me/avatar` on the self lane) returning `{ image_base64 }`, which the console renders as a `data:`
URL; a principal without a picture is a **404**. It is deliberately JSON and **not** a raw `image/jpeg`
handler ([ADR-0018](/architecture/decisions/#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)):
a raw-bytes chi handler would sit **outside** the Huma authz middleware, breaking the
permission-on-every-route invariant, and a bare `<img src>` cannot carry a bearer, so a token-only session
could not authenticate it. The typed client fetches the JSON (a session cookie or a bearer both work) and
builds the data URL.

## The node path

Nodes do not use general role x scope. A node authenticates with a per-tenant **NATS JWT/nkey** credential bound to its `node.name` and is authorized only to **its own assignments**: publish telemetry, heartbeat, consume the commands addressed to it. It is an identity-scoped narrow path, and the scope is carried by **NATS subject permissions**, not a route authorizer:

- A node is a NATS client over the WAN (outbound only). The connection resolves the principal (kind=`node`) from the nkey, and the JWT's subject permissions are the node's placement-derived `visible_set`: it may publish only to its own ingress and report subjects and consume only from its own durable command queue. The general RBAC permission matrix does not apply.
- Datapoints land on the JetStream **raw ingress** subject (the admission consumer confines owner to the trusted stream); the node receives commands from a durable, server-side JetStream command queue rather than polling a route. Placement (the [cascade](/architecture/cascade/)) compiles directly into the account's subject grants, so a node can address only the owners it actually covers.
- A node's published datapoints are owner-bound at **stream-consume time, ahead of any evaluation**, by the **admission consumer** at the head of the data lane: for a node it checks the payload owner against the node's placement-derived `visible_set`; for a central webhook, against the interface's declared owner (the per-class confinement is specified in [messaging](/architecture/messaging/)). It re-publishes only confined datapoints to the trusted stream the rule engine, calc, and persistence sink consume; an owner outside the set is an orphan / discovery candidate, never an authoritative datapoint (see [collection](/architecture/collection/)). The fence cannot live only at the durable write, because the rule engine consumes the stream **live**: a forged owner must be caught **before** it can open an alarm or fire an action. **Trusted server-internal producers** (calc, the action layer's intended write) publish to the trusted stream directly, no admission pass. The admission consumer itself runs in **system mode** (its owner lookup is a system-mode gateway read); the persistence sink is then a trusted **system mode** `COPY` relying on confined owners upstream, with no per-row scope predicate of its own.

A `node` credential whose subject permissions do not cover a subject is rejected by NATS at publish/subscribe time; a non-`node` principal cannot hold a node account's subject grants.

## One model, never duplicated

Authorization is **two in-app layers, each enforced in one place and re-derived nowhere else**: the `<resource>:<action>` **capability** check runs as API route middleware before the handler, and the **ABAC scope** filter is injected by the Storage Gateway on every query (a row filter belongs at the data path, where it holds by construction; the gateway also writes the in-transaction `audit_log`). The gateway owns **scope and audit**, not capability. The invariant is that no third surface re-implements either:

- **The live UI relay calls these, it does not copy them.** Operators never connect to NATS. The SSE subscribe is a normal route, **capability fast-rejected** at open (not authorized there); the server-side [SSE relay](/architecture/messaging/) then runs each candidate message through the **same** gateway scope a read uses, filtering by `visible_set(P, read)` against each message's exclusive-arc owner, so a live tile gets exactly the rows the operator could have fetched. The session **re-checks on every grant-cache invalidation** for its principal and closes if `:read` is lost, so a mid-stream scope shrink tears the stream down rather than leaking.
- **Node subject permissions gate the subject; the admission consumer gates the owner.** A node's NATS grants are mechanically derived from its placement as a coarse transport gate on the WAN edge. But subject permissions constrain the subject **string**, while a datapoint's owner lives in the **payload** (a multi-owner function resolves owner from labels server-side), so the subject grant is **not** a redundant copy of the owner fence: the **admission consumer** (above) is the authoritative owner fence, checking the payload owner against placement at consume time. Subject perms keep a node off subjects it has no business on; the admission consumer keeps a forged owner label out of the trusted stream. The bus carries no operator (`kind=human`) clients at all; an AI tool acting as one reaches the platform only through the API.

## Encryption in transit

TLS on the HTTP API (terminated at the binary when given a cert + key, or at the operator's reverse proxy) and on the NATS connection that carries node telemetry and commands. **BYO PKI.** "TLS off" is a deliberate dev-mode flag, never a silent default.

## Audit

Every API operation records the resolved **actor** (the principal id) in `audit_log`. Secret decrypts are always audited, never filterable. Node-mode writes record the node principal as actor; system-mode writes record `actor = 'system'` (or `'bootstrap'` for the seed phase) so the audit trail distinguishes operator action from platform internals. An AI tool acts via OAuth as a `human` or `service` principal, so its writes record that principal as actor. When a request is **impersonated**, the row also records `real_actor_principal_id`, the true admin behind the impersonated actor, so accountability survives impersonation (see [impersonation](#impersonation-view-as-and-act-as)).

**Two write paths, one read path.** Estate mutations write their `audit_log` row **in the same transaction** as the change (via the Storage Gateway, so a committed change always has its audit row). **Auth events** (login, logout) fire on read/no-tx paths, so they emit through a separate non-transactional seam and record `resource = 'auth'`. The read side is `GET /audit-log` (newest first, filterable by resource and verb, backward-paged by a `before` timestamp), which resolves each actor and real-actor to a username. It is **admin/owner-only**: the route requires the admin-sensitive `audit:read:admin` (three tokens), which a two-token wildcard like `viewer`'s `*:read` cannot match, so only `admin` (which carries it explicitly) or `owner` (`>`) reaches it ([ADR-0015](/architecture/decisions/#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)), and a read-only operator cannot see the security trail. **Failed sign-ins** are captured for accountability: a wrong password on a **real** account is `login_failed` (attributed to that principal, a brute-force signal), and a correct password against a **disabled** account is `login_denied`. An attempt on an **unknown** username is deliberately **not** written, so scanning random usernames cannot flood the log; bounding a targeted brute force against a real account is the job of endpoint rate limiting (a later slice), not suppressing the audit.

## Bootstrap

The first install runs `omniglass bootstrap <username>` with optional `--password <pw>`, `--email <email>`, and `--display-name <name>` flags. This creates the first operator as a `human` principal with an `owner @ all` grant and a bearer credential (the cleartext token is shown once, only `sha256(token)` is stored) in one transaction; with `--password` it also installs a password credential (argon2id, PHC-encoded) so the owner can sign in to the web console. That operator logs in via the web UI or CLI and begins minting other principals. There is no implicit default principal; the bootstrap is the only path to the first owner.

## Worked example

Sam is an AV support tech. SCIM syncs Sam into the **`AV-Support`** `principal_group` (or Sam is a local `human` principal). The group holds one grant: `operator @ "AV-devices" (component-group), viewer @ "HQ" (location)`. Result:

- Sam can **operate** (create / update / ack alarms) on AV devices fleet-wide (the cross-cutting entity-group), and **read** everything at HQ (the location node + its subtree).
- The gateway's scope filter hides every row outside those scopes; the API middleware blocks Sam from, say, creating a principal (no `principal:create` capability in `operator`).
- The day a device joins the `AV-devices` dynamic group, it enters Sam's scope; the day Sam leaves `AV-Support` in the IdP, SCIM removes the grant.

:::caution[Open question]
The SCIM mapping detail: which IdP attributes drive `principal_group` membership and grants.
:::

## Storage

The IAM subjects and their grants; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `principal` (+ per-kind `human` / `service` / `node`) | id, kind | base `principal` is identity (opaque uuid) + kind only; per-kind tables hold the rest, including each kind's label: `human.display_name` (the person's real name) + username + email, the `service` label, the `node` name (+ labels, last_heartbeat_at, bound credential) |
| `role` | id, **official**, permissions (jsonb: `<resource>:<action>`) | RBAC capability set; ship viewer/operator/admin/owner + custom |
| `principal_grant` | (principal_id, role, **scope**) | role x scope; scope = a structural node, an entity-group, or `all`; additive |

