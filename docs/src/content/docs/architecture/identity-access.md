---
title: Identity and access
description: How principals authenticate, how grants combine roles with scopes, and how the app enforces RBAC and ABAC entirely in the Storage Gateway.
sidebar:
  badge:
    text: Spec
    variant: caution
---

Identity and access is how an operator controls who may call the platform and which slice of the estate each caller can see and act on, enforced entirely in the app so "forgot to filter" cannot happen. Enforcement lives in the Storage Gateway (the only path to the database); scope is built on the cascade's groups ([cascade](/architecture/cascade/)). This doc says what IAM **is**.

## The model in one breath

A **principal** is the polymorphic subject of authN/authZ. Identity is the principal's opaque uuid, never an email or name. Each principal has one or more **credentials** (how it authenticates). Each principal holds zero or more **grants**, each a `(role x scope)` pair: the role contributes the verbs, the scope contributes the entities. Permissions are **additive** across grants. The API middleware checks RBAC capabilities before the handler runs; the Storage Gateway injects ABAC scope on every query.

## Principal kinds

A principal carries a `kind` value; the same role machinery works across all kinds. Identity is uniform; authN methods and per-kind domain attributes differ.

| kind | what it represents | authN |
|---|---|---|
| `human` | a person | local password + session, OIDC, SAML |
| `service` | scripts, integrations, SDKs, bots | bearer token |
| `node` | the edge daemon running in the field | bearer token, mTLS |

There is **no `ai` principal kind**. AI does not get its own broad identity; it acts on a user's behalf via **OAuth on-behalf-of (delegation)**, scoped and audited to the granting user (see [AI acts via delegation](#ai-acts-via-delegation) below and the [AI](/architecture/ai/) page).

Each kind that needs structured domain attributes gets a **1:1 per-kind table** linked by `principal_id`: `human`, `service`, and `node`. The base `principal` table holds identity + kind only; the per-kind tables hold the rest, including the kind's human-facing label (a human's `display_name`, a service's label, the node's name).

## Credentials

One `credential` row per authN method per principal. A principal can hold many (a human with a password + an OIDC link; a service with a rotating token). `(method, identifier)` is the lookup key.

| method | identifier | secret_hash | who uses it |
|---|---|---|---|
| `password` | `principal.id` (uuid) | argon2id of the password | humans |
| `oidc` | `iss\|sub` (issuer + subject) | null (IdP verifies) | humans |
| `token` | `sha256(token)` | null (identifier IS the verifier) | service / node |
| `mtls` | client-cert subject | null (TLS verifies) | nodes |

The password identifier is the `principal.id` (not the username), so a username change does not invalidate the credential. Bearer tokens are 256-bit `crypto/rand` payloads with a human-readable prefix (`ogs_` for services, `ogn_` for nodes) for secret-scanners and audit clarity; the server only ever stores `sha256(token)`. Cleartext is returned exactly once at mint time.

:::caution[Open question]
OIDC delegates MFA to the IdP; whether to add a local-account TOTP path for installs not on OIDC is
undecided.
:::

## Subjects

`human`, `service`, `node`, and **`principal_group`s**. Roles attach to principals regardless of kind; the same `principal_grant` rows mean the same thing whether the principal is a person, a service, or a daemon.

## Group kinds

The `group` membership mechanism (static list or dynamic filter) is shared across kinds, but the kinds are kept **distinct** (not one polymorphic primitive yet, because their usage differs):

- **`component` / `system` / `location` groups** are **entity-groups**: they carry config bindings (the cascade) and serve as ABAC **scopes**.
- **`principal_group`** is a collection of principals (SCIM-synced or local): a grant **subject**, carrying no config. It groups over principals, not just humans (members can be any principal kind); in practice it is humans synced from the IdP.

So `group` appears on **both sides of authZ**: `principal_group`s as subjects, entity-groups as object scopes.

:::caution[Open question]
Whether to unify the group kinds into a single polymorphic `group` primitive; revisit if their usage
converges.
:::

## AI acts via delegation

AI is **not** a principal kind. When an agent acts, it acts **on behalf of a user** via **OAuth on-behalf-of (delegation)**: it holds a delegated, scoped, audited credential and operates strictly within the granting user's permissions and scope. It cannot exceed the user who delegated to it, and the resulting write names both the agent and that user, so accountability lands on a human.

This is the **chosen mechanism** for AI acting. Omniglass acts as an OAuth OP, issuing delegated credentials with dual-actor audit: AI = delegation, not a broad identity of its own. The [AI](/architecture/ai/) page covers the capability spectrum this governs.

## Roles and the role hierarchy

A role is a **capability set**: permissions per `(resource, action)`. Roles live in a `role` table keyed by a globally unique `id`, each carrying an **`official` boolean**:

- **`official: true`**: ship-with the binary, seeded via the boot phase. A release can patch a default permission via `ON CONFLICT DO UPDATE` on the seed.
- **`official: false`**: operator-created via the IAM API.

**No overrides**: a role id is globally unique across both kinds (the create paths refuse an `official: false` role whose id matches an `official: true` one, and the seed phase fails-safe with a loud warning if it would collide with an existing operator role). This is a deliberate divergence from `datapoint_type` (where an org-scoped key may shadow an official one of the same name): role override risks lockout with no compensating use case, so a role id resolves to exactly one row.

### The four official roles

```
viewer    <-  operator  <-  admin  <-  owner
```

Linear inheritance (transitive): each role's effective permissions are the union of its own permissions and all transitively-inherited roles' permissions.

| role | what it can do |
|---|---|
| `viewer` | Read every operator-facing resource within scope. |
| `operator` | viewer + create/update on components, interfaces, tasks, rules, config; ack/snooze/resolve alarms. |
| `admin` | operator + delete on managed resources + manage IAM (principals, credentials, grants, custom roles). Cannot delete `official` roles. |
| `owner` | god mode (`*:*`). The unkillable role: at least one active `owner@all` grant must exist at all times (enforced by DB trigger). The bootstrap creates the first owner; only an owner can revoke another owner. |

### Custom roles

Operators create `official: false` roles via the IAM API with a chosen permission set, optionally inheriting from `viewer` (or any other role). Inheritance rules:

- An `official: true` role may inherit only from other `official: true` roles (enforced at seed time).
- An `official: false` role may inherit from any role.

Because of the no-override rule, `inherits: [viewer]` is unambiguous (every id resolves to exactly one role).

### Permission format

Permissions are strings: `<resource>:<action>`. One entry per resource per role; actions are comma-separated; wildcards stand alone.

```
component:read                <- single action
component:create,update       <- multiple actions, one resource
alarm:ack,snooze,resolve      <- domain verbs alongside CRUD
principal:*                   <- any action on this resource
*:*                           <- any action on any resource (owner only)
```

Actions are HTTP-aligned: `read` (GET), `create` (POST), `update` (PATCH/PUT), `delete` (DELETE), plus resource-specific verbs (`ack`, `snooze`, `resolve` for alarms; future kinds add their own). The aggregate `write` does not exist as an alias; `*` is the wildcard and reads as honestly.

Inheritance composes permissions **per resource by union of actions**:

```
parent: component:create,update
child:  component:delete
child effective:  component:{create, update, delete}
```

There are no negative permissions. To narrow a parent's capability set, define a fresh role rather than inherit.

:::caution[Open question]
Whether to add custom-role permission granularity beyond `(resource x action)` (e.g. a Zoom-style
data-claim suffix `<resource>:<action>:<modifier>`), pending a use case.
:::

## Authorization: grants = role x scope

A principal holds grants in `principal_grant`. Each grant is a `(role, scope_kind, scope_id)` triple. A principal can hold many grants; they are **additive**:

```
canDo(P, action, E)  iff  exists grant g in grants(P) such that
                            action in perms(g.role)
                            AND E in expand(g.scope_kind, g.scope_id)
```

So the same role applied at different scopes composes naturally; mixing roles (e.g., `operator @ HQ` + `viewer @ all` for a site lead who needs read-only visibility outside their primary site) is the intended pattern. Grants from `principal_group` memberships compose the same way.

### Scopes

| scope_kind | scope_id | expansion |
|---|---|---|
| `all` | null | every entity in the database |
| `location` | location id | subtree(L): L + its systems + their components + descendants |
| `system` | system id | subtree(S): S + its components + descendants |
| `component` | component id | exactly { C } |
| `group` | group id | members(G) at resolution time (dynamic groups re-resolve) |

`scope_kind` is enumerated (`all`, `location`, `system`, `component`, `group`); adding a new kind requires a schema change (CHECK constraint) and a new case in the gateway's `expand` function. `scope_id` is operator data.

:::caution[Open question]
Whether a scope may mix include and exclude (e.g. "all except group X").
:::

## Visibility cascades down the structural tree

A scope of entity E includes E **and everything structurally beneath it** (a location -> its systems -> their components -> their datapoints and alarms). So `visible_set` = the union, over a grant's scopes, of each scope entity plus its descendants. Dynamic-group scopes recompute as membership changes. The set is bounded by **fleet size (entities)**, not data volume.

## The owner invariant

At least one active `owner @ all` grant must exist at all times. Enforced as a deferrable constraint trigger in Postgres (fires at `COMMIT`, so the swap-owners pattern works in one transaction):

```
BEGIN;
  INSERT INTO principal_grant (... role='owner', scope_kind='all' ...);  -- new owner
  DELETE FROM principal_grant WHERE principal_id=<old> AND role='owner';  -- old
COMMIT;  -- trigger fires here, sees the new grant, passes.
```

Attempting to remove the last owner (by grant delete, principal delete, principal disable, or role change) raises a check-violation. The Gateway translates this into a 400 with a clear remediation message.

## Enforcement: two layers, both in the app

There is **no RLS and no direct database access** (no PostgREST). The **Storage Gateway is the only door to the database** and the API is its only caller, so authz lives entirely in the app:

- **Capability (RBAC) in the API middleware** -> can this principal perform this action on this resource kind at all? Answered from an in-process cache of the principal's effective permissions, rejected before the gateway. Routes declare their required permission with `rbac.Require("component:create")`.
- **Scope (ABAC) in the Storage Gateway** -> every query carries the principal's resolved scope, and the gateway filters rows by their exclusive-arc owner against the visible set (the owning `component`/`system`/`location` in the `visible_set`) on reads and the same predicate on writes. The visible set is bounded by **fleet size (entities), not data volume**, so it stays an indexed membership filter even on the firehose; and because it is an owner filter in app code, not a DB policy, it works identically on Postgres, the columnar tier, or object storage.
- The gateway has two query **modes**: **scoped** (an API request carrying a principal's visible set) and **system** (trusted internal work: ingest / engine / reconcile / migrate / seed, all-visibility). System mode is an explicit, audited choice, never the default. There is no third path: any storage caller is one of these two.
- **Scope is structural, not per-handler**: the principal's scope is a required input to the gateway's query layer, so no code path can query unscoped by accident. With no RLS backstop for in-database scope the gateway is the sole guarantor, so "forgot to filter" must be impossible by construction, not by discipline.
- **Non-entity resources** (the `datapoint_type` registries, roles, principals, groups) are **capability-gated globally**, no entity scope applies (typically admin only).

Both layers operate **within one database**. Tenant isolation is **per-database** (CNPG-per-tenant): there is no `tenant_id` column anywhere, so the cross-tenant boundary is the database boundary itself, not a row predicate. Intra-database scope (above) is the only app-enforced layer; there is no RLS backstop.

## Caching strategy

The hot path must not hit the DB for RBAC. Three layers, in-process, no persisted "effective permissions" projection (which would invite cache-coherence bugs):

1. **Role index**: at boot, the `role` table is loaded into a Go map with `inherits` resolved transitively and wildcards expanded. Refreshed on `LISTEN/NOTIFY` from `role` table changes.
2. **Principal cache**: at session establish (or first token-auth), the principal's grants are loaded and effective permissions computed as a `Set[resource:action]`. Cached by `principal_id`. Invalidated on `LISTEN/NOTIFY` from `principal_grant` or `principal` changes.
3. **Per-request**: middleware does a Set membership check on the cached permissions plus, for the gateway, a scope expansion (visible_set). Both O(1)-with-a-prefactor in the common case.

The DB is the source of truth; caches are derived views with explicit invalidation events. `LISTEN/NOTIFY` keeps the design single-binary-friendly; the same invalidation contract scales to NATS / Redis pub-sub when we shard.

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

`permissions` is flat and wildcard-expanded, ready for O(1) `useCan(...)` checks in the web app. `grants` is for advanced UI logic (showing scope chips, explaining why a button is or is not shown).

## The node path

Nodes do not use general role x scope. A node authenticates with a credential bound to its `node.name` (a bearer token or mTLS) and is authorized only to **its own assignments**: pull my worklist, heartbeat, push for my tasks. It is an identity-scoped narrow path at the API:

- The middleware resolves the principal (kind=`node`) as usual.
- The node-route group (`/api/v1/nodes/{name}/*`, gRPC ingest) is wrapped in a narrow authorizer: `principal.kind == 'node'` AND `principal.credential.identifier == {name}` from the URL. The general RBAC permission matrix does not apply to this group.
- Behind the gateway, node-driven writes run in **system mode** (since the node is operating on platform internals on behalf of itself).

A `node` principal attempting any general API route returns 403; a non-`node` principal hitting a node route returns 403.

## Encryption in transit

TLS on the HTTP API and the gRPC ingest, terminated at the binary (it serves HTTPS when given a cert + key) or at the operator's reverse proxy. **BYO PKI.** "TLS off" is a deliberate dev-mode flag, never a silent default.

## Audit

Every API operation records the resolved **actor** (the principal id) in `audit_log`. Secret decrypts are always audited, never filterable. System-mode writes record `actor = 'system'` (or `'bootstrap'` for the seed phase) so the audit trail distinguishes operator action from platform internals.

## Bootstrap

The first install runs `og iam create-owner --username ops --email ops@example.com`. This creates the first operator as a `human` principal, a password credential (argon2id), and an `owner @ all` grant in one transaction. That operator logs in via the web UI or CLI and begins minting other principals. There is no implicit default principal; the bootstrap is the only path to the first owner.

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

