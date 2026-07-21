---
title: API
description: "The API contract: AIP-style resources and :verb methods, cursor lists, a problem+json error envelope, idempotent writes, and long-running operations carried by the action row."
sidebar:
  badge:
    text: Partial
    variant: note
---

The contract is **two typed surfaces, one source of truth**. The **public HTTP / OpenAPI contract** (this
page) is the north face: every operator action, every integration, the SPA, the CLI, and the
[MCP](#also-an-mcp-surface) server go through it, and it is the only caller of the
[Storage Gateway](/architecture/storage/). The **internal and edge transport is a sibling NATS subject
contract** (subjects, message schemas, request-reply, JetStream stream and consumer definitions), the
service-to-service and node wire; it is typed and versioned the same way and lives in
[messaging](/architecture/messaging/). This page is the **contract every HTTP route honors**. The doctrine
behind it (the API is the source of truth, the clients are generated from it) and the generation pipeline
live in [API first](/contributing/api-first/); this page is the conventions that doctrine points at.

:::note[Partial]
Built today: the Huma-over-chi API with the OpenAPI 3.1 document generated from the Go structs
(`make gen`), the AIP-style resource and `:verb` routing, and the problem+json error envelope, proven
on `/auth`, `/roles`, `/locations`, `/systems`, `/components`, `/nodes`, `/interfaces`, `/tasks`, and the
per-component reachability read, plus the type registries. The node `:enroll` and `:claim` custom methods
are the first `:verb` routes in the wild. Still `Design`:
the expression `filter` language, idempotency keys, long-running operations over the `action` row, the
MCP surface, the SSE relay, and the NATS node contract. See [implementation status](/architecture/status/).
:::

## Shape: resources and `:verb` methods

Everything lives under `/api/v1`. The path shape is derivable, not special-cased:

- **Plural resource collections**, standard methods by primary key (AIP-style): `POST` creates (409 on
  PK collision), `GET` reads, `PATCH` partial-updates (AIP-134), `DELETE` removes. No upsert shortcuts.
- **Custom methods carry a colon**, `:verb` not `/verb`, for anything that is not CRUD:
  `/alarms/{id}:ack`, `/components/{name}:apply`, `/views/{id}:run`. The verb
  is also the **permission**: `:ack` is gated by `alarm:ack`, so the route and the
  [authorization](/architecture/identity-access/) check share one vocabulary. The **self-scoped**
  `/auth/me` family is the exception: `/auth/me:changePassword`, `/auth/me/sessions/{id}:revoke`, and
  the bulk `/auth/me/sessions:revokeAll` (a `{ purpose }` body, keeping the current credential) are
  **authn-only** (they resolve the target from the session, never a path id, so they carry no
  capability and a credential id that is not the caller's own is a 404, not a cross-principal action).
  The **admin** counterparts on `/principals/{id}` do carry a capability and a scoped path id:
  `GET /principals/{id}/sessions` and `POST /principals/{id}/sessions/{sid}:revoke` (both gated by
  `principal:revoke-session`) let an administrator list and end **another** principal's sessions, the revoke
  bounded to that target and behind the owner takeover guard. `POST /principals/{id}/sessions:revokeAll` (a
  `{ purpose }` body, same gate and guard) bulk-ends all of one kind at once, returning the count.
- **Singular kind sub-segments** for the typed families: `/rules/calc`, `/datapoints/metric`,
  `/types/component`.
- **Collection-level custom methods** carry the colon on the collection, not a member:
  `POST /systems:checkName` (also `/components:checkName`, `/locations:checkName`) is an advisory
  precheck for a technical-name rename, returning `{ valid, available, reason }`. It is gated by
  `<entity>:update` like a rename, but its availability answer is deliberately **scope-blind**: the
  `name` uniqueness constraint is global, so a scope-filtered answer would report a name held outside
  the caller's scope as free and then 409 at save. This is a bounded, documented exception to the
  ABAC-scope-on-every-query rule (it discloses only that a technical name is taken somewhere, nothing
  more), not a license to skip scope elsewhere.
- **A principal is addressable by uuid or username.** Every `/principals/{id}` route (read, update,
  grants, the lifecycle verbs, reset, sessions, impersonate) accepts either the principal's uuid or a human's
  current username, resolved server-side (a value that parses as a uuid is used directly; otherwise it
  is a username lookup, and an unknown one is a 404). The uuid is still the stable identity (a username
  is mutable and nothing keys on it), so a username is a convenience address resolved at call time.
  Service principals have no username and stay uuid-addressed.

## Lists: filter, order, page

A list takes `filter`, `order_by`, `page_size` (capped by a server maximum), `page_token`, and `fields`:

- **Cursor pagination, never offset.** A list returns a `next_page_token`; the client echoes it on the
  next call. The token is opaque and stable under concurrent inserts, where an offset would skip or
  repeat rows.
- **`filter` is one [Omniglass expression](/architecture/expressions/)** over the resource's fields, the
  same language as rule scopes and dynamic groups, so an operator learns it once.
- **`filter`, `order_by`, and `fields` name fields, not raw SQL.** Every field resolves through the
  gateway's generated-column allow-list (an unknown field is a 400), and values are bound parameters, so
  none of the three can inject SQL ([storage](/architecture/storage/)).
- **Every list runs through the scoped gateway**, so results are already scope-filtered: a list never
  returns a row outside the caller's visible set, and the page count is over visible rows only.

## Partial responses: field masks

The `fields` parameter selects a subset of the response (a read field mask, AIP-157); the default is the
full resource. `PATCH` carries a **write mask implicitly**: only the fields present in the body change,
so a partial update never clobbers an omitted field.

:::caution[Open question]
Field-mask depth: top-level fields only, or nested paths (`a.b.c`), and whether a list's `fields` and a
get's `fields` share one grammar.
:::

## Errors: one problem+json envelope

Every error is **RFC 9457 `application/problem+json`**: `type`, `title`, `status`, `detail`, `instance`,
plus an Omniglass `code` (a stable machine string) and, for validation, a `violations` array of
`{field, message}`. One shape, so the generated client and the CLI render every failure uniformly. The
status mapping:

| Status | Meaning |
|---|---|
| 400 | malformed request (bad JSON, an undeclared param) |
| 401 | unauthenticated |
| 403 | **action denied on this target**: the principal lacks the capability entirely, or can read the target but not perform this action on it (below) |
| 404 | not found, **including out-of-read-scope** (below) |
| 409 | conflict: PK collision, a stale conditional write, or an idempotency replay mismatch |
| 422 | semantic validation (the `:apply` unmet-required-inputs case) |
| 429 | throttled |

**The 403/404 split is three-way, by where the target sits in the caller's
[per-action scope](/architecture/identity-access/).** (a) The action is in **no** grant the principal
holds: **403**, capability missing entirely. (b) The target is in the caller's **read-scope** but outside
`visible_set(P, action)` for the requested action (the principal can `GET` it but cannot `:ack` it):
**403**, which leaks nothing because the caller can already read the row. (c) The target is **outside the
caller's read-scope** entirely: **404**, so the API never discloses that an entity exists outside the
caller's visible set. Out-of-read-scope is the only 404 case; a readable-but-not-actionable target is a
403, never a 404.

## Idempotency and concurrency

- **`Idempotency-Key`** is accepted on `POST` and on state-changing custom methods. The server records
  the key with its **effect** (the created or changed resource) for a retention window; a retry with the
  same key returns the original outcome, not a duplicate, so a flaky network never produces two components
  or a double `:ack`. **Only successful (2xx) outcomes are memoized.** An authorization result
  (401 / 403 / 404) is **never** stored against the key; it is re-evaluated against current grants on every
  call, so a denial recorded before an access change is not re-served, and a success is never replayed
  after a grant is revoked: a replay **re-enters the authorization and gateway path** before the memoized
  effect is returned. Re-evaluation guards the replay, not the original effect, which already committed.
- **Optimistic concurrency**: a conditional update carries the resource version (an `ETag` / `If-Match`);
  a write against a stale version is a 409, never a silent last-writer-wins.

:::caution[Open question]
The idempotency-key retention window, and whether it is uniform or per-method.
:::

## Long-running operations: the action is the handle

Some operations are not instantaneous: a `command` against a device, a reconcile `:enforce`, a
credential rotation, a multi-step flow. These do **not** block the request and do **not** introduce a
parallel `operations` resource. The custom method **returns an [`action`](/architecture/alarms-actions/)
row** (its id and status), the same stateful entity the response layer already uses, and the caller polls
`GET /actions/{id}` through `queued -> sent -> done` / `failed`. The action **is** the operation handle,
so "fire and follow" is one model whether the trigger was a rule or an API call. A fast operation may
inline its result when it finishes within the request, but the handle is always returned, so a slow
device never holds the connection open. The action row is ABAC-owned by its target's exclusive-arc owner,
so polling `GET /actions/{id}` is read-scoped to whoever can see the target, independent of the per-action
scope that launched it.

The HTTP method is the front door; the **dispatch is over NATS**. The command stays HTTP-exposed (returns
the handle, poll `GET /actions/{id}`), but the work is carried on the internal NATS contract: the action
fans out through [messaging](/architecture/messaging/) to the responsible consumer or node, and the result
flows back the same way to advance the row. The caller sees one model, the transport is the bus.

## Writes are audited and scoped

- Every write emits an [`audit_log`](/architecture/audit/) row in the **same transaction** as the
  change, a gateway responsibility, so it cannot be forgotten or bypassed.
- Every route **declares its permission** (checked before the handler runs) and every query **carries the
  caller's scope** (injected by the gateway). Both are [identity and access](/architecture/identity-access/)
  invariants, and the API is the gateway's only caller, so there is no unscoped path. That declared permission
  is also **published in the generated spec**: each gated operation carries an `x-omniglass-permission`
  extension (for example `role:read:admin` on `GET /roles`), so `api/openapi.json` is a machine-readable map of
  the authz contract, and the set of all stamps is the **permission universe** the [Roles
  view](/architecture/identity-access/#the-permission-universe-published-per-route) reports.

## The collection surface: nodes, interfaces, tasks

The [collection](/architecture/collection/) authoring routes are the first concrete resources that exercise
every convention above at once: standard methods by primary key, the first `:verb` custom methods, the
non-disclosing 404, a declared permission per route, and injected scope per query. They ship in the AIP
shape the [Shape](#shape-resources-and-verb-methods) section describes.

| Method | Path | Permission |
|---|---|---|
| GET | `/nodes` | `node:read` |
| GET | `/nodes/{name}` | `node:read` |
| POST | `/nodes` | `node:create` |
| PATCH | `/nodes/{name}` | `node:update` |
| DELETE | `/nodes/{name}` | `node:delete` |
| POST | `/nodes/{name}:enroll` | `node:enroll` |
| POST | `/nodes:claim` | none (public) |
| GET | `/interfaces` | `interface:read` |
| GET | `/interfaces/{id}` | `interface:read` |
| POST | `/interfaces` | `interface:create` |
| PATCH | `/interfaces/{id}` | `interface:update` |
| DELETE | `/interfaces/{id}` | `interface:delete` |
| GET | `/tasks` | `task:read` |
| GET | `/tasks/{id}` | `task:read` |
| GET | `/components/{name}/reachability` | `component:read` |
| GET | `/components/{name}/events` | `component:read` |

**The node custom methods are the day-one enrollment handshake.** `POST /nodes/{name}:enroll` mints (or
re-mints) the node's enrollment token and returns it **once**; the server stores only its hash and never
logs it, so a re-enroll invalidates the previous token. It is gated by `node:enroll`, the verb-is-the-permission
rule. `POST /nodes:claim` is the **node-facing** side of the exchange: a node presents its token and receives
its NATS credential (url, username, password). It is the surface's **one public route**, unauthenticated
because the token itself is the authentication, so it carries no permission and an invalid token is a
**401** (a claim must not disclose which nodes exist). A node is estate-wide, so `node:read` and `node:create`
require an **all-scope** grant, not a tree-scoped one.

**The interface is authored; the task is derived.** An interface is addressed by a surrogate `id` and is
**named by its protocol**: its `name` derives from its `interface_type` and is unique **within its component**
(so create takes a type, not a name, and a duplicate protocol on one component is a **409**). Creating an
interface **derives its one poll task**, so the task surface is **read-only** (`GET /tasks`, `GET /tasks/{id}`):
there are no task create, update, or delete routes and no `task:create` / `task:update` grants. A task references
its interface by `interface_id`, its id is **content-addressed** over its interface, mode, and spec, and it
carries **no node column**: its placement **projects from the interface**. An interface belongs to a component
(or is server-hosted, which needs an all-scoped grant), and a task belongs to an interface, so both inherit the
component's [scope](/architecture/identity-access/): an out-of-read-scope component's interface or task is a
non-disclosing **404**, exactly the [403/404 split](#errors-one-problemjson-envelope) above.

**The reachability read is a typed composed read, not yet a view.** `GET /components/{name}/reachability`
composes, per interface, the latest verdict state (`interface.reachable`), the probe-layer signals that
compose it (the raw `icmp`/`tcp` metrics), and the recent verdict transitions the availability strip reads.
It is gated by `component:read` and scope-injected through the component, so an out-of-scope component is a
non-disclosing 404 and the datapoint reads only ever run on a verified, in-scope component. It is a
hand-written typed `GET`, an early and deliberate exception to [reads beyond one resource are
views](#reads-beyond-one-resource-are-views), standing in until the `ViewResult` framework lands.

**The event read is the log-kind mirror of the reachability read.** `GET /components/{name}/events` returns
the component's recent **log occurrences** (the [`event` log sink](/architecture/core-entities/#the-event-sink-the-first-arc-owned-occurrence)),
newest first, bounded to the last 24 hours and capped at 200 rows. Each row carries its `ts`, the property
`key` (e.g. `syslog.line`), the `instance` discriminator, the `message`, optional structured `attributes`,
its `provenance` (`observed` for direct collection), and the `source` interface type. It is gated by
`component:read` and scope-injected through the same `GetComponent` gate as the reachability read, so an
out-of-scope component is the same non-disclosing 404 and the event read only ever runs on a verified,
in-scope component. Like reachability, it is a hand-written typed `GET` standing in until the `ViewResult`
framework lands.

:::note[Thin cuts today]
These routes ship the operationally useful slice, not the full CRUD matrix. A **node** has create, list, get,
`:enroll`, and `:claim`, but no update or delete; a node **purge cascades** its interfaces and their derived
tasks. An **interface** `PATCH` changes only its node placement and its params (target); the type (and so the
name it derives) and the owning component are fixed at creation, and a delete is refused while a task still
references it (a **409**). A **task** is **derived and read-only**: it is created with its interface and has no
write routes, and its placement follows the interface's rather than being set on the task. The four built
interface types are `icmp`, `tcp`, `ssh`, and `http`; there is no `interface_type` list route yet.
:::
## Secrets: masked reads, an audited reveal

A **secret** is a typed, encrypted-at-rest operator value ([config, credentials, and
variables](/architecture/variables/)), and its routes are a worked instance of the conventions above:
the AIP resource plus a `:verb` custom method, the verb-is-the-permission rule, the implicit `PATCH`
write mask, same-transaction audit, and a scoped read. The registry and the directory read ride the
**viewer read floor** (`secret:read`, which `*:read` satisfies);
the three writes gate on `secret:create` / `secret:update` / `secret:delete`; the plaintext decrypt
gates on **`secret:reveal`**, a permission the `*:read` floor does **not** carry, so a plain "read
everything" grant sees only masks and **only admin (`secret:*`) and owner (`>`) reveal**. Every
`:reveal` writes an [audit](/architecture/audit/) row (verb `reveal`) in the same call.

- `GET /types/secret` lists the shape registry, each `{id, display_name, official, fields:[{name, type,
  secret, origin}]}` (`secret:read`).
- `GET /secrets` is the **all-scope admin directory** (`{secrets: [secret]}`); like the principal
  directory it needs an all-scope grant, and a non-all scope is a 403 (`secret:read`).
- `POST /secrets` creates one from `{name, secret_type, owner_kind: global|location|system|component,
  owner?, fields}` (201, `secret:create`); a `global` secret needs an all-scope grant.
- `PATCH /secrets/{id}` re-seals the given `fields`, merged over the stored value so an omitted field
  keeps its value (`secret:update`).
- `DELETE /secrets/{id}` removes it (204, `secret:delete`).
- `POST /secrets/{id}:reveal` returns the decrypted `{fields: {name: plaintext}}` (`secret:reveal`,
  audited).

A secret's fields are masked in every read: the `secret` body (`{id, name, secret_type, owner_kind,
owner_id?, owner_name?, fields:[{name, value, secret}]}`) returns `••••••` for a secret field, and only
`:reveal` returns plaintext.

A **variable** is the plaintext sibling of a secret ([config, secrets, and
variables](/architecture/variables/)): the same owner arc and cascade, but shown in the clear (no
registry, no mask, no reveal). The directory read rides the viewer floor
(`variable:read`); `POST` / `PATCH` gate on `variable:create` / `variable:update` (granted to
operators); `DELETE` gates on `variable:delete` (admin, owner). The value is polymorphic JSON typed by
`value_type`.

- `GET /variables` is the **all-scope admin directory** (`{variables: [variable]}`); like the secret
  directory it needs an all-scope grant, and a non-all scope is a 403 (`variable:read`).
- `POST /variables` creates one from `{name, value_type: string|int|float|bool|json, owner_kind:
  global|location|system|component, owner?, value}` (201, `variable:create`); a `global` variable needs
  an all-scope grant, and the `value` is validated against `value_type`.
- `PATCH /variables/{id}` replaces the `value` (validated against the fixed `value_type`;
  `variable:update`).
- `DELETE /variables/{id}` removes it (204, `variable:delete`).

A `variable` body is `{id, name, value_type, owner_kind, owner_id?, owner_name?, value}`, the `value` in
the clear.

A **tag** ([tags](/architecture/tags/)) is a `key: value` label, and its routes split along the
governance line: **minting a key** is a tenant-wide governance action, but **setting a value** is the
owning entity's own write. The key vocabulary and an entity's tags read on the viewer floor
(`tag:read`, `component:read`).

- `GET /tags` lists the governed key vocabulary (`{tags: [tag]}`, `tag:read`); a `tag` body is `{id,
  name, applies_to, propagates}`.
- `POST /tags` mints a key from `{name, applies_to?, propagates?}` (201, `tag:create`, all-scope); the
  name is normalized to a lowercase identifier (a 422 otherwise), `applies_to` is an entity-kind
  allow-list (empty = universal), and `propagates` defaults true.
- `PATCH /tags/{name}` replaces a key's `{applies_to?, propagates?}` (`tag:update`, all-scope); the name
  is fixed.
- `DELETE /tags/{name}` removes a key, cascading its bindings (204, `tag:delete`, all-scope).
- `POST /tags/{name}:setGlobal` sets the **global** value for a key from `{value}` (`tag:update`);
  `POST /tags/{name}:clearGlobal` removes it (204). A global binding has no owning entity, so it gates on
  `tag:update`.
- `GET /{components,systems,locations,nodes}/{name}:listTags` lists the bindings set **directly** on one entity
  (`{tags: [tagBinding]}`, the entity's `:read`).
- `POST /{components,systems,locations,nodes}/{name}:setTag` binds a value from `{key, value}` on the entity;
  the key must exist and its `applies_to` must admit the kind (a 422 otherwise). Setting a value is the
  entity's own write, so it gates on the entity's **`:update`** (`component:update` and friends), not a
  tag permission. `POST /{...}/{name}:removeTag` from `{key}` removes the binding (204). Bindings are
  custom methods on the entity (like the principal lifecycle) rather than a nested collection, so the
  generated CLI stays collision-free.
- `GET /components/{name}/effective-tags` is the **cascade** for one component: each a `resolvedTag`
  (`{key, value, owner_kind, owner_id?, owner_name?, band, depth, winner}`), keys unioning and values
  overriding most-specific-wins, with the winner and shadowed candidates. A non-propagating key resolves
  only from a binding on the component itself (`component:read`; the component must be in the caller's
  component read-scope).
- The directory list routes (`GET /components`, `/systems`, `/locations`) each carry an **`effective_tags`**
  map (`{key: winning_value}`, winners only) on every row, resolved for the whole page in one batched query.
  It feeds the Tags column. A component resolves the full arc; a location resolves global plus its location
  tree; a system resolves global, its system tree, and the location it is placed at. Provenance lives in the
  per-entity effective-tags detail, not the row.

A `tagBinding` body is `{key, value, owner_kind, owner_id?, owner_name?}`.

The **component-classification catalogs** ([core entities](/architecture/core-entities/#catalog-reference-data-vendor-driver-capability))
are Catalog reference data, flat official-vs-custom registries a future `product` layer will reference,
on the same pattern as the `*_type` registries. Each is its own resource with the same CRUD shape: the
list and read routes sit on the viewer floor (`vendor:read` / `driver:read` / `capability:read`, which
`*:read` carries); the three writes gate on `<resource>:create` / `<resource>:update` /
`<resource>:delete`, all at the admin tier, exactly like `type:*`. An **official** (seed-owned) row is
read-only (`PATCH` and `DELETE` both 422).

A **vendor** (Crestron, Biamp, ...) names an organization, generalizing the former manufacturer-only
`component_make` with a **`kind`** of `manufacturer` / `integrator` / `developer` (default
`manufacturer`, a 422 for any other value).

- `GET /vendors` lists the registry, ordered alphabetically by display name (`{vendors: [vendor]}`,
  `vendor:read`).
- `POST /vendors` mints a custom vendor from `{id, display_name, kind?, icon?, support_phone?, website?}`
  (201, `vendor:create`, admin).
- `GET /vendors/{id}` reads one (`vendor:read`).
- `PATCH /vendors/{id}` updates `{display_name?, kind?, icon?, support_phone?, website?}` (`vendor:update`,
  admin).
- `DELETE /vendors/{id}` removes a custom vendor (204, `vendor:delete`, admin).

A `vendor` body is `{id, display_name, kind, icon, support_phone, website, official}`. `website` is
validated to an `http`/`https` scheme on write (a 422 for any other scheme, for example `javascript:`).

A **driver** (Generic SNMP, Cisco xAPI, ...) names the implementation that gets, emits, or sets a
product's signals, with an optional **`version`**.

- `GET /drivers` lists the registry, ordered alphabetically by display name (`{drivers: [driver]}`,
  `driver:read`).
- `POST /drivers` mints a custom driver from `{id, display_name, version?}` (201, `driver:create`, admin).
- `GET /drivers/{id}` reads one (`driver:read`).
- `PATCH /drivers/{id}` updates `{display_name?, version?}` (`driver:update`, admin).
- `DELETE /drivers/{id}` removes a custom driver (204, `driver:delete`, admin).

A `driver` body is `{id, display_name, version, official}`.

A **capability** (Microphone, Display, ...) names what a component can do.

- `GET /capabilities` lists the registry, ordered alphabetically by display name
  (`{capabilities: [capability]}`, `capability:read`).
- `POST /capabilities` mints a custom capability from `{id, display_name}` (201, `capability:create`,
  admin).
- `GET /capabilities/{id}` reads one (`capability:read`).
- `PATCH /capabilities/{id}` updates `{display_name?}` (`capability:update`, admin).
- `DELETE /capabilities/{id}` removes a custom capability (204, `capability:delete`, admin).

A `capability` body is `{id, display_name, official}`.

A **product** ([core entities](/architecture/core-entities/#catalog-reference-data-product)) is the
concrete **SKU** that ties the leaf catalogs together: a **vendor** (who makes it), a **driver** (what
talks to it), a **kind** (`device` / `app` / `service` / `vm`, default `device`, a 422 for any other
value), an optional **parent** product (a variant), and the **capabilities** it provides. It is the
layer the catalogs above were built for, and the target of `component.product_id`. Its writes gate on
`product:create` / `product:update` / `product:delete` at the admin tier; the list and read routes sit
on the viewer floor (`product:read`, which `*:read` carries). An **official** (seed-owned) row is
read-only (`PATCH` and `DELETE` both 422).

- `GET /products` lists the registry, ordered alphabetically by display name (`{products: [product]}`,
  `product:read`). Each row carries its vendor, driver, kind, and capabilities.
- `POST /products` mints a custom product from
  `{id, display_name, kind?, vendor_id?, driver_id?, parent_product_id?, capabilities?}` (201,
  `product:create`, admin).
- `GET /products/{id}` reads one, with its capabilities (`product:read`).
- `PATCH /products/{id}` updates
  `{display_name?, kind?, vendor_id?, driver_id?, parent_product_id?, capabilities?}` (`product:update`,
  admin); `capabilities`, when given, **replaces** the whole set.
- `DELETE /products/{id}` removes a custom product (204, `product:delete`, admin); an official row is
  refused (422), and a product still referenced by a component is refused (409).

A `product` body is
`{id, display_name, kind, vendor_id, driver_id, parent_product_id, capabilities, official}`. An unknown
vendor / driver / parent / capability reference is a 422.

## Files: content-addressed bytes behind a handle

A **file** is a searchable handle over a content-addressed [blob](/architecture/files/): the metadata is
tenant-wide (no placement arc), so unlike a secret these routes take **no scope**, only the
`file:<action>` permission plus the per-file `sensitive` tier. Reading rides the **viewer floor**
(`file:read`, which `*:read` carries, since a file is not a sensitive *resource*); a **sensitive** file is
instead fenced to the `:admin` tier (`file:read:admin`), hidden from a lister without it and a
**non-disclosing 404** to a reader without it, exactly the [secret sensitivity rule](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier).
The bytes ride **base64 in JSON** on both create and download (the [avatar precedent](/architecture/decisions/#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)),
so the whole surface stays under the authz middleware and generates a uniform client.

- `GET /files` is the directory (`{files: [file]}`), sensitive files omitted below the admin tier
  (`file:read`).
- `POST /files` creates one from an upload `{name, content_type, content (base64), sensitive?}` (201,
  `file:create`): the server hashes the bytes, **deduplicates** the blob, and writes the handle. A
  `sensitive: true` file additionally needs the admin tier.
- `GET /files/{id}` returns one handle's metadata (`file:read`); a sensitive file is a non-disclosing 404
  without the admin tier.
- `GET /files/{id}:download` returns `{name, content_type, content (base64)}`, the blob read back and its
  hash verified (`file:read`).
- `DELETE /files/{id}` removes the handle (204, `file:delete`); the blob is freed in the same transaction
  when no other handle references it (dedup-aware, so storage is reclaimed), and a blob still shared by
  another handle is kept.

A `file` body is `{id, name, content_type, size, sha256, sensitive, created_at}`; the `sha256` is the
content address of the blob it points at, so two handles over identical bytes share one blob.

## Reads beyond one resource are views

A single resource reads through its typed `GET`. Anything richer, a dashboard, an explorer, the cascade
"why did this value win" view, goes through a **[view](/architecture/views/)**: a named query returning a
uniform `ViewResult` (`{columns, rows}`), bound by declared params at `/views/{id}:run`, executed through
the same scoped gateway. Views are part of the public API; an operator never gets raw SQL. A **live** read
(a tile that streams) may upgrade from polling `:run` to a **server-relayed [SSE](/architecture/messaging/)
stream** over the same scoped, permission-gated seam: the subscribe is **capability fast-rejected** at open
(not authorized there), then the server holds the internal subscription and re-runs the gateway scope per
message, filtering by `visible_set(P, read)` against each message's owner and pushing only visible deltas.
The operator never connects to the bus,
so the live path adds no second authorization model.

## Versioning and evolution

The path carries the major version (`/api/v1`). Within a version, change is **additive only**: new
fields, new optional params, new resources, never a removal or a meaning change; a breaking change is a
new major version, not a silent edit. Because the [OpenAPI 3.1 document is generated](/contributing/api-first/)
from the Go structs and the clients are generated from that, the contract cannot drift from the
implementation: a drift check fails the PR if a route changed without regenerating.

## Also an MCP surface

The same OpenAPI document that generates the typed SPA client and the CLI also generates an **MCP
server**, one more [generated client](/contributing/api-first/) over the same gateway, so an AI
[agent](/architecture/ai/) drives the platform through the exact seams a human does: every tool call is
the same route permission, the same gateway scope, the same same-transaction [audit](/architecture/audit/).
It is **not a side channel**.

The binding is mechanical, but the **tool catalog is curated, not a raw one-method-per-tool dump**:
task-oriented tools, the [views](/architecture/views/) exposed as search and query tools (the richest
reads), pagination and the problem+json errors shaped for a model to consume. The MCP server runs under
the **authenticated `human` or `service` principal's** credential
([identity and access](/architecture/identity-access/)), so its reach is exactly that principal's grants,
scoped and audited like any caller ([AI](/architecture/ai/)).

## The node path is the NATS contract

Nodes do **not** speak HTTP. The edge is a NATS client over the WAN: a node publishes telemetry to a
JetStream stream, consumes its commands from a durable server-side JetStream command queue, and is enrolled by a NATS
JWT/nkey, all on the sibling **NATS subject contract**, not this page's routes. The old node HTTP custom
methods (the heartbeat, the telemetry post) are gone; their wire is now subjects and message schemas. The
proto definitions survive **as the NATS message schema**, the typed shape on the bus. That contract,
subjects, request-reply, stream and consumer definitions, JWT-scoped subject permissions, is documented in
[messaging](/architecture/messaging/) and on the [node](/architecture/nodes/) page; the same AIP spirit,
error envelope, and idempotency described here carry across to it (the idempotency key per message, the
problem-shaped reply on request-reply).

## Self-describing

The running server serves `GET /api/v1/openapi.json`, `/openapi.yaml`, and a human reference page, so the
public contract is discoverable live against any deployment, not only in these docs. The internal NATS
subject contract is self-describing the same way: its subjects, message schemas, and stream and consumer
definitions are published from the running server, the sibling of OpenAPI for the bus.

Related: [API first](/contributing/api-first/) (the doctrine and the generation pipeline),
[messaging](/architecture/messaging/) (the sibling NATS subject contract and the bus),
[identity and access](/architecture/identity-access/) (permission + scope), [audit](/architecture/audit/)
(the write-time record), [UI](/architecture/ui/) (the views BFF and the renderer contract), and
[expressions](/architecture/expressions/) (the `filter` language).
