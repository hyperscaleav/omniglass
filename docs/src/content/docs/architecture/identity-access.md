---
title: Identity and access
description: How principals authenticate, how grants combine roles with scopes, and how the app enforces capability at the route and ABAC scope in the Storage Gateway.
sidebar:
  badge:
    text: Design
    variant: caution
---

Identity and access is how an operator controls who may call the platform and which slice of the estate each caller can see and act on, enforced entirely in the app so "forgot to filter" cannot happen. Enforcement is **two in-app layers**: the capability check (`<resource>:<action>`) runs as **API route middleware** before the handler, and the **ABAC scope** filter is injected by the **Storage Gateway** (the only path to the database), where a row-level filter holds by construction. Scope is built on the cascade's groups ([cascade](/architecture/cascade/)). This doc says what IAM **is**.

## The model in one breath

A **principal** is the polymorphic subject of authN/authZ. Identity is the principal's opaque uuid, never an email or name. Each principal has one or more **credentials** (how it authenticates). Each principal holds zero or more **grants**, each a `(role x scope)` pair: the role contributes the verbs, the scope contributes the entities. Permissions are **additive** across grants. The API middleware checks RBAC capabilities before the handler runs; the Storage Gateway injects ABAC scope on every query.

## Principal kinds

A principal carries a `kind` value; the same role machinery works across all kinds. Identity is uniform; authN methods and per-kind domain attributes differ.

| kind | what it represents | authN |
|---|---|---|
| `human` | a person | local password + session, OIDC, SAML |
| `service` | scripts, integrations, SDKs, bots | bearer token |
| `node` | the edge daemon running in the field | NATS JWT/nkey credential |

**AI actors act as a user today; a first-class `agent` principal is deferred.** An AI tool authenticates via **OAuth as a `human` or `service` principal** and acts with exactly that principal's grants, no separate identity. A dedicated `agent` principal kind (first-class AI identity, bounded by a sponsoring human, with a propose -> approve gate on mutations) may be added when the capability matures; it is deliberately deferred so the platform does not carry that authorization machinery for a feature a year out. This note is the one place that direction is recorded; everywhere else AI is simply a scoped, audited user ([AI](/architecture/ai/)).

Each kind that needs structured domain attributes gets a **1:1 per-kind table** linked by `principal_id`: `human`, `service`, and `node`. The base `principal` table holds identity + kind only; the per-kind tables hold the rest, including the kind's human-facing label (a human's `display_name`, a service's label, the node's name).

## Credentials

One `credential` row per authN method per principal. A principal can hold many (a human with a password + an OIDC link; a service with a rotating token). `(method, identifier)` is the lookup key.

| method | identifier | secret_hash | who uses it |
|---|---|---|---|
| `password` | `principal.id` (uuid) | argon2id of the password | humans |
| `oidc` | `iss\|sub` (issuer + subject) | null (IdP verifies) | humans |
| `token` | `sha256(token)` | null (identifier IS the verifier) | service |
| `nats` | nkey public key | null (NATS verifies the signed nonce) | nodes |

The password identifier is the `principal.id` (not the username), so a username change does not invalidate the credential. Service bearer tokens are 256-bit `crypto/rand` payloads with a human-readable prefix (`ogs_`) for secret-scanners and audit clarity; the server only ever stores `sha256(token)`. Cleartext is returned exactly once at mint time. A `node` enrolls with a per-tenant **NATS JWT/nkey** instead: the credential row stores the nkey public key, NATS verifies a signed nonce, and the JWT carries the node's subject permissions (its placement-derived `visible_set`, see [The node path](#the-node-path)).

:::caution[Open question]
OIDC delegates MFA to the IdP; whether to add a local-account TOTP path for installs not on OIDC is
undecided.
:::

## Subjects

`human`, `service`, `node`, and **`principal_group`s**. Roles attach to principals regardless of kind; the same `principal_grant` rows mean the same thing whether the principal is a person, a service, a daemon, or an AI tool acting as one.

## Group kinds

The `group` membership mechanism (static list or dynamic filter) is shared across kinds, but the kinds are kept **distinct** (not one polymorphic primitive yet, because their usage differs):

- **`component` / `system` / `location` groups** are **entity-groups**: they carry config bindings (the cascade) and serve as ABAC **scopes**.
- **`principal_group`** is a collection of principals (SCIM-synced or local): a grant **subject**, carrying no config. It groups over principals, not just humans (members can be any principal kind); in practice it is humans synced from the IdP.

So `group` appears on **both sides of authZ**: `principal_group`s as subjects, entity-groups as object scopes.

:::caution[Open question]
Whether to unify the group kinds into a single polymorphic `group` primitive; revisit if their usage
converges.
:::

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
| `admin` | operator + delete on managed resources + manage IAM (principals, credentials, grants, custom roles) + curate registries (`<registry>:create`). IAM management is meaningful only from an `@ all` grant (a scoped `admin @ subtree` keeps the operator powers within its subtree but gets no IAM); registry curation is a plain capability, so a custom role can carry `<registry>:create` alone for a non-admin curator. Cannot delete `official` roles. |
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
datapoint_type:create         <- a registry curator capability (tag/unit/event_type/severity_level/source likewise)
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

## Visibility cascades down the structural tree

A scope of entity E includes E **and everything structurally beneath it** (a location -> its systems -> their components -> their datapoints and alarms). The visible set is **parameterized by action**: `visible_set(P, action)` = the union, over **only the grants whose role carries `action`**, of each scope entity plus its descendants. There is no single global visible set. **`:read` is an implicit floor on every grant**: holding any grant on an entity confers `read` on it, so `visible_set(P, read)` is always the widest set and `visible_set(P, action)` is always a subset of it. The floor is realized as a **capability injection at role-index build** (next): every `<resource>:<action>` permission implies `<resource>:read`, so the implied reads are present in the fast-reject union, in `canDo`'s `perms`, and in `/auth/me.permissions`, not only in the scope layer. A verb-only role (`alarm:ack` without `alarm:read`, no `viewer` inheritance) is therefore **not** hard-403'd on the read. The asymmetry runs one way only: a principal can **read** an entity it cannot **act** on (in `visible_set(P, read)` but outside `visible_set(P, ack)`, via a read-only grant), but never the reverse. So there is no "actionable but not readable" case, and the status split below stays three-way. Dynamic-group scopes recompute as membership changes. Each per-action set is bounded by **fleet size (entities)**, not data volume.

## The owner invariant

At least one active `owner @ all` grant must exist at all times. Enforced as a deferrable constraint trigger in Postgres (fires at `COMMIT`, so the swap-owners pattern works in one transaction):

```
BEGIN;
  INSERT INTO principal_grant (... role='owner', scope_kind='all' ...);  -- new owner
  DELETE FROM principal_grant WHERE principal_id=<old> AND role='owner';  -- old
COMMIT;  -- trigger fires here, sees the new grant, passes.
```

Attempting to remove the last owner (by grant delete, principal delete, principal disable, or role change) raises a check-violation. The Gateway translates this into a 400 with a clear remediation message.

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

**Worked example (per-grant binding denies estate-wide ack).** Principal P holds two grants: `operator @ group-A` (role carries `alarm:ack`) and `viewer @ all` (read-only). Alarm X is owned by a component in **group-B**. P calls `POST /alarms/X:ack`:

1. **Middleware fast-reject**: `alarm:ack` appears in *a* grant (the `operator @ group-A` one), so it passes. (This is why fast-reject is necessary-not-sufficient: it cannot see that the ack-carrying grant does not cover X.)
2. **Up-front `canDo(P, ack, X)`**: the only grant whose role carries `ack` is `operator @ group-A`; X is not in `expand(group-A)`. `viewer @ all` carries `ack` = no. So `canDo` = **false**.
3. **Status**: X is in `visible_set(P, read)` (via `viewer @ all`) but outside `visible_set(P, ack)`. Branch (b): **403**, "cannot ack this alarm", not a 404 (P can already `GET /alarms/X`, so non-disclosure does not apply).
4. **Backstop**: had the handler skipped step 2, the gateway's `:ack` write carries `visible_set(P, ack)`, X is outside it, the UPDATE matches 0 rows, and the gateway returns the same 403, never a silent success.

The flattened-set model would have wrongly allowed this: `ack` is "in the permission set" and X is "in the global visible set", so the per-grant binding is exactly what stops estate-wide ack.
- **Non-entity resources** have no entity `E`, so `canDo` cannot scope by owner. Two governance classes:
  - **IAM subjects** (`principal`, `role`, `principal_grant`, and a principal's **login credential** create/delete): the action must appear in a grant whose `scope_kind` is `all`. A scoped grant confers **no** IAM capability, so `role:create` carried by an `operator @ HQ` grant does not let you create roles. Typically `owner @ all` / `admin @ all`. (Device secrets are a different resource: a **credential variable** is entity-scoped, so its `secret:read` plaintext decrypt and its rotation are ordinary scoped actions against the credential's owner, [config and credentials](/architecture/variables/).)
  - **Data registries** (`datapoint_type`, `tag`, `unit`, `event_type`, `severity_level`, source): governed by a distinct **`<registry>:create` curator capability** (`datapoint_type:create`, `tag:create`, `unit:create`, `event_type:create`, `severity_level:create`, `source:create`). A registry entry has no owner entity, so the grant's `scope_kind` is irrelevant: the check is simply whether the principal holds the capability. Granting it to a curator role lets a principal mint registry entries **without** IAM admin; a minted entry carries its own `scope` (an org-scoped entry shadows an official one, the [namespace-shadow pattern](/architecture/datapoints/#key-scope-template-org-official)), and `official`-scoped entries are reserved to `owner` and the boot seed.

  The fast-reject still only rejects; for these resources the authorization is the grant-class check (an `all`-scoped grant for IAM, the `<registry>:create` capability for registries), the one place the decision is capability-shaped because there is no entity to scope.

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

`permissions` is flat and wildcard-expanded, ready for O(1) `useCan(...)` checks in the web app. It is a **fast-reject / UI hint only**, the union over all grants: it answers "could this principal ever do X anywhere", never "can it do X to **this** entity". List visibility likewise (a row in `GET /alarms` is read-scoped) does **not** imply per-action authority on that row. Per-row action affordances (the ack/snooze button on a specific alarm) must be computed against `visible_set(P, action)` for that target, which the `grants` array drives: `grants` is the source for advanced UI logic (scope chips, deciding per-row actionability, explaining why a button is or is not shown). The server is the only authority regardless; the flat list and the list view are hints, the scoped gateway decides.

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

Every API operation records the resolved **actor** (the principal id) in `audit_log`. Secret decrypts are always audited, never filterable. Node-mode writes record the node principal as actor; system-mode writes record `actor = 'system'` (or `'bootstrap'` for the seed phase) so the audit trail distinguishes operator action from platform internals. An AI tool acts via OAuth as a `human` or `service` principal, so its writes record that principal as actor.

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

