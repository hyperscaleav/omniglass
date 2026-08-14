---
title: API
description: "The API contract: AIP-style resources and :verb methods, cursor lists, a problem+json error envelope, idempotent writes, and long-running operations carried by the action row."
sidebar:
  badge:
    text: Partial
    variant: note
---

The contract is **two typed surfaces, one source of truth**. The **public HTTP / OpenAPI contract**
(this page) carries every operator action, integration, the SPA, the CLI, and the
[MCP](#also-an-mcp-surface) server, and is the only caller of the
[Storage Gateway](/architecture/storage/); the **internal and edge transport is a sibling NATS subject
contract**, typed and versioned the same way ([messaging](/architecture/messaging/)). The doctrine and
generation pipeline: [API first](/contributing/api-first/); this page is the conventions every HTTP
route honors.

:::note[Partial]
Built today: the Huma-over-chi API with the OpenAPI 3.1 document generated from the Go structs
(`make gen`), the AIP-style resource and `:verb` routing, and the problem+json error model
([ADR-0068](/architecture/decisions/#adr-0068-the-api-error-model-is-the-stock-rfc-9457-shape)), proven
on `/auth`, `/roles`, `/locations`, `/systems`, `/components`, `/nodes`, `/interfaces`, `/tasks`, the
first-party ingest write `POST /telemetry:push` (gated `telemetry:push`, owner declared in the body and
fenced by the caller's scope), the per-component reachability read, the type registries, the
`/products` and `/standards` catalogs, the classifier-contract and instance-value property routes, the
role declaration, resolution, and staffing routes, and the component alarm plus system and location
[health](#health-the-verdict-and-why) reads. The node `:enroll` and `:claim` custom methods are the
first `:verb` routes in the wild. See [implementation status](/architecture/status/).
:::

## Shape: resources and `:verb` methods

Everything lives under `/api/v1`. The path shape is derivable, not special-cased:

- **Plural resource collections**, standard methods addressed by the entity's `name` where it has one
  and by its uuid where it does not (AIP-style): `POST` creates (409 when the `name` is already taken
  in its placement bucket; the uuid primary key is the server's to mint and cannot collide), `GET`
  reads, `PATCH` partial-updates (AIP-134), `DELETE` removes. No upsert shortcuts. On `location`,
  `system`, and `component`, the `{ref}` path parameter takes a third form beside uuid and bare name: a
  dotted address, a positional lookup resolved structurally before any scope or ambiguity check runs
  ([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)).
- **Every name-bearing body carries both handles**: a uuid **`id`** (stable identity, the target every
  foreign key stores) and a unique, renameable **`name`** (the identifier an operator reads and
  types). A create takes the `name`; the uuid is the database's to mint, and **every write returns
  it**, so a client stores the id and detects a rename by diffing the id it holds
  ([ADR-0076](/architecture/decisions/#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)).
  A path or a reference resolves whichever form it is given, since a kebab name can never look like a
  uuid.
- **A rename is a custom method, never a `PATCH` field.** `POST /components/{name}:rename` (also
  `/systems`, `/locations`, `/principal-groups/{id}`) moves the `name`, gated by `<entity>:rename`
  rather than `<entity>:update`, and the `PATCH` body of those four carries no `name` at all. Renaming
  breaks every reference an operator stored outside the system (a bookmark, a runbook step, an
  integration's config), so it is an act a grant can withhold on its own instead of a side effect of
  editing a label.
- **A move is a custom method too**
  ([ADR-0088](/architecture/decisions/#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)).
  `POST /components/{name}:move` and `/systems/{name}:move` take `{location?, parent?}` (at least one
  required, 422 otherwise); `/locations/{name}:move` takes `{parent}` only, with no clear-to-root
  branch (a `location` has never had one). Both follow the house three-state convention (omitted
  unchanged, `""` clears, a name sets), gated by `<entity>:move` rather than `<entity>:update`, and the
  `PATCH` body of all three carries neither `parent` nor `location` any more. Clearing `parent` to
  root under `:move` requires an all-scoped grant, the same authorization creating a root row already
  requires: a `PATCH` used to skip that check entirely on the clear branch. `:move` writes its own
  audit verb, `move`, and never recomputes health except where the rollup genuinely depends on the
  placement it changes: a system's own `location` field, which keeps moving health at both ends
  exactly as its old combined `PATCH` did, and a location's `parent`, which carries its whole
  subtree's contribution between two ancestor chains and recomputes both
  ([ADR-0092](/architecture/decisions/#adr-0092-a-location-move-recomputes-both-ancestor-chains)).
- **Custom methods carry a colon**, `:verb` not `/verb`, for anything that is not CRUD:
  `/components/{name}/commands:issue`, `/auth/me/sessions/{id}:revoke`, `/nodes:claim`. The verb
  is also the **permission**: `:issue` is gated by `command:issue`, so the route and the
  [authorization](/architecture/identity-access/) check share one vocabulary. The **self-scoped**
  `/auth/me` family is the exception: authn-only, resolving the target from the session, never a path
  id, so it carries no capability and a credential id not the caller's own is a 404; the **admin**
  counterparts on `/principals/{id}` do carry a capability and a scoped path id.
- **Each typed registry is its own plural collection**: `/location-types`, `/metric-types`,
  `/property-types`,
  `/event-types` ([ADR-0060](/architecture/decisions/#adr-0060-a-resource-is-one-kebab-case-noun-nesting-means-ownership)).
- **Collection-level custom methods** carry the colon on the collection, not a member:
  `POST /systems:checkName` (also `/components:checkName`, `/locations:checkName`) is an advisory
  precheck for a technical-name rename, returning `{ valid, available, reason }`, gated by
  `<entity>:update` (the advisory reads a name's availability; performing the rename needs
  `<entity>:rename`). Name uniqueness is scoped to **placement**, not the whole estate
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627)): the request carries the same
  `parent`/`location` fields a create would, and availability is checked against that specific
  placement bucket, not a single global fact. It stays **blind to the caller's own grant scope**:
  a deploy principal scoped to one subtree still gets a correct answer for a placement it cannot
  read via `GET`, because the check queries the target bucket directly rather than reading the row.
- **A principal is addressable by uuid or username.** Every `/principals/{id}` route resolves either
  server-side (a value that parses as a uuid is used directly, else a username lookup, an unknown one
  a 404). The uuid stays the stable identity (a username is mutable, nothing keys on it); service
  principals have no username and stay uuid-addressed.

## Lists: filter, order, page

The built lists take a fixed query-param set (`kind`, `resource`, `verb`, `system`,
`include_archived`, `include_cleared`), and the one paginated route, `GET /audit-log`, pages backward
with `before` plus `limit`. **Every list runs through the scoped gateway**: a list never returns a row
outside the caller's visible set, and the page count is over visible rows only.

:::design[The AIP list parameters, tracked in #522]
The target contract: a list takes `filter`, `order_by`, `page_size` (capped by a server maximum),
`page_token`, and `fields`:

- **Cursor pagination, never offset.** A list returns a `next_page_token`; the client echoes it on the
  next call. The token is opaque and stable under concurrent inserts (an offset would skip or repeat
  rows).
- **`filter` is one [Omniglass expression](/architecture/expressions/)** over the resource's fields, the
  same language as rule scopes and dynamic groups.
- **`filter`, `order_by`, and `fields` name fields, not raw SQL.** Every field resolves through the
  gateway's generated-column allow-list (an unknown field is a 400), and values are bound parameters, so
  none of the three can inject SQL ([storage](/architecture/storage/)).
:::

## Partial responses: field masks

:::design[The fields read mask, tracked in #522]
The `fields` read mask (a response subset, AIP-157) selects the fields a read returns; the default is
the full resource.
:::

Today the full resource is the only read behavior. The **write mask** is built, AIP-134 exactly
([ADR-0091](/architecture/decisions/#adr-0091-an-update_mask-says-which-fields-a-patch-writes)):

- **Omit `update_mask`** and the mask is **implied**: the fields the body populated (a non-empty
  value) change, and an omitted field is left alone. This is what every `PATCH` in the tree has always
  done, so it is the behavior of all of them.
- **Send `update_mask`** and exactly the fields it names change, **populated or not**, so a named
  field the body leaves empty is **cleared**. This is the only way to clear a field that has no
  empty-value sentinel of its own, an integer or a list where "empty" and "absent" look the same on
  the wire.
- **`update_mask: ["*"]`** is **full replacement**, the equivalent of a `PUT`: every patchable field
  is written, so one the body omits goes back to its default. It cannot be combined with named
  fields.
- A mask naming a field the resource does not patch is a **422 naming the field**, never a silent
  no-op.

The mask rides in the **request body**, not the query string: this API has no gRPC transcoding to
force the split, Huma models a body as a typed struct, and a body field generates cleanly into both
the typed client and a CLI flag. It is top-level field names only, spelled as the body spells them.

The **three-state string** convention (an omitted field unchanged, an explicit `""` clears, a value
sets: `moveComponentInput`, a system's `standard`) is untouched and stays the idiomatic way to clear a
string. The mask is what generalizes clearing to everything that is not a string; retiring the
sentinel in its favor is a separate ripple, not folded in here.

The **inherited registry facts** are where that pays off. A `component_type` or `system_type` stores
`stem`, `abbrev`, `icon` and `label_rule` as nullable strings where NULL means "inherit from the
nearest ancestor that sets one", so each of the four has a clear state, and all four spell it the
same way ([#716](https://github.com/hyperscaleav/omniglass/issues/716)): omit the field and it is
unchanged, send `""` and the column goes back to NULL so the walk resumes, send a value and it sets.
Both edit blades send `""` for an empty box, which is what makes an emptied box mean what it
displays. The instrument is the sentinel rather than the mask because a nullable string has an empty
value to overload, the exact distinction ADR-0106 draws when it sends objects to the mask instead.

Two things the clear does not do. `stem` keeps its character rule on the patch, now written
`^([a-z0-9][a-z0-9-]*)?$` with `minLength` gone, so `""` is admitted and every malformed stem is
still a 422; only the clear got in, not a relaxation. And a **root** type cannot clear its stem:
there is no ancestor behind it, so the refusal that guards create guards the clear, 422 naming the
reason.

**Clearing a nullable OBJECT field is the mask, always** (`name_rule` is the first,
[ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)).
An object has no empty value to overload the way a string has `""`: `{}` is a rule with default
fields, not the absence of one, and an explicit `null` is indistinguishable from an omitted key
once the body is decoded into a typed struct. So the field goes in `update_mask` and the body
carries no value for it:

```http
PATCH /location-types/{id}
{ "update_mask": ["name_rule"], "name_rule": null }
```

Sending the `null` is optional and reads well; the mask is what carries the intent. This is the
convention for every nullable object field that follows, not a `name_rule` special case.

Built on the role declarations (`PATCH /standards/{id}/roles/{role}`, `PATCH
/systems/{name}/roles/{role}`) and adopted by `PATCH /location-types/{id}`. The other `PATCH` routes
accept no mask yet, and they do not need one to stay correct: an absent mask is the implied mask,
which is exactly what they already do.

:::caution[Open question]
Field-mask depth: top-level fields only (what the write mask ships), or nested paths (`a.b.c`), and
whether a list's `fields` and a get's `fields` share one grammar.
:::

## Errors: one problem+json envelope

Every error is **RFC 9457 `application/problem+json`**, in Huma's stock shape: `title`, `status`,
`detail`, and, for validation, an `errors` array of `{location, message, value}` details, so every
client renders every failure uniformly; the sketched custom envelope is retired
([ADR-0068](/architecture/decisions/#adr-0068-the-api-error-model-is-the-stock-rfc-9457-shape)). The
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
`visible_set(P, action)` (the principal can `GET` it but cannot `:ack` it): **403**, which leaks nothing
because the caller can already read the row. (c) The target is **outside the caller's read-scope**
entirely: **404**, the only 404 case, so the API never discloses that an entity exists outside the
caller's visible set.

::::design[Idempotency keys and optimistic concurrency, tracked in #522]
## Idempotency and concurrency

- **`Idempotency-Key`** is accepted on `POST` and on state-changing custom methods. The server records
  the key with its **effect** for a retention window; a retry with the same key returns the original
  outcome, not a duplicate. **Only successful (2xx) outcomes are memoized**, never an authorization
  result (401 / 403 / 404): a replay **re-enters the authorization and gateway path** before the
  memoized effect is returned, so a stale denial is not re-served and a success is never replayed
  after a grant is revoked.
- **Optimistic concurrency**: a conditional update carries the resource version (an `ETag` / `If-Match`);
  a write against a stale version is a 409, never a silent last-writer-wins.

:::caution[Open question]
The idempotency-key retention window, and whether it is uniform or per-method.
:::
::::

:::design[Long-running operations over the action row, tracked in #522]
## Long-running operations: the action is the handle

A non-instantaneous operation (a `command` against a device, a reconcile `:enforce`, a credential
rotation) does **not** block the request and does **not** introduce a parallel `operations` resource:
the custom method **returns an [`action`](/architecture/alarms-actions/) row** (its id and status),
and the caller polls `GET /actions/{id}` through `queued -> sent -> done` / `failed`. The action **is**
the operation handle, so "fire and follow" is one model whether the trigger was a rule or an API call;
a fast operation may inline its result, but the handle is always returned. The action row is ABAC-owned
by its target's exclusive-arc owner, so polling is read-scoped to whoever can see the target. The HTTP
method is the front door; **dispatch is over NATS**: the action fans out through
[messaging](/architecture/messaging/) and the result flows back to advance the row.
:::

## Writes are audited and scoped

- Every write emits an [`audit_log`](/architecture/audit/) row in the **same transaction** as the
  change, a gateway responsibility, so it cannot be forgotten or bypassed.
- Every route **declares its permission** (checked before the handler runs) and every query **carries the
  caller's scope** (injected by the gateway), both [identity and access](/architecture/identity-access/)
  invariants; the API is the gateway's only caller, so there is no unscoped path. Each gated operation
  carries an `x-omniglass-permission` extension (for example `role:read:admin` on `GET /roles`), making
  `api/openapi.json` a machine-readable map of the authz contract; the set of stamps is the
  **permission universe** the [Roles
  view](/architecture/identity-access/#the-permission-universe-published-per-route) reports.

## The collection surface: nodes, interfaces, tasks

The [collection](/architecture/collection/) authoring routes are the first concrete resources that
exercise every convention above at once: the standard family per resource (`/nodes`, `/interfaces`,
`/tasks`) plus the `:verb` custom methods and the per-component reads (`reachability`,
`reconciliation`, `events`), all `component:read`-gated. The routes live in the
[generated API reference](/reference/api/), rendered from the OpenAPI document on every build, each
operation's description naming its permission gate verbatim (a guard test enforces that, alongside the
spec-contract test that every gated operation carries its `x-omniglass-permission` stamp and
`POST /nodes:claim` stays the deliberate public write).

**The node custom methods are the day-one enrollment handshake.** `POST /nodes/{name}:enroll` (gated
`node:enroll`) mints or re-mints the node's enrollment token and returns it **once**; the server stores
only its hash and never logs it, so a re-enroll invalidates the previous token. `POST /nodes:claim` is
the **node-facing** side: a node presents its token and receives its NATS credential (url, username,
password). It is the surface's **one public route**, unauthenticated because the token itself is the
authentication, and an invalid token is a **401** (a claim must not disclose which nodes exist). A node
is estate-wide, so `node:read` and `node:create` require an **all-scope** grant.

**The interface is authored; the task is derived.** An interface is addressed by a surrogate `id` and
**named by its protocol**: `name` derives from `interface_type`, unique **within its component**
(create takes a type, not a name; a duplicate protocol on one component is a **409**). Creating an
interface **derives its one poll task**, so the task surface is **read-only** (`GET /tasks`,
`GET /tasks/{id}`): no task write routes or grants. A task references its interface by
`interface_id`, its id **content-addressed** over interface, mode, and spec, with **no node column**:
placement **projects from the interface**. An interface belongs to a component (or is server-hosted,
needing an all-scoped grant), a task to an interface, so both inherit the component's
[scope](/architecture/identity-access/): an out-of-read-scope component's interface or task is a
non-disclosing **404**.

**Three per-component reads stand in until the `ViewResult` framework lands**, each a hand-written
typed `GET`, gated `component:read` and scope-injected through the same `GetComponent` gate, so an
out-of-scope component is a non-disclosing 404 (a deliberate early exception to
[reads beyond one resource are views](#reads-beyond-one-resource-are-views)):

- `GET /components/{name}/reachability` composes, per interface, the latest verdict state
  (`interface-reachable`), the probe-layer signals that compose it (the raw `icmp`/`tcp` metrics), and
  the recent verdict transitions the availability strip reads.
- `GET /components/{name}/events` is the log-kind mirror: the component's recent **log occurrences**
  (the [`event` log sink](/architecture/core-entities/#the-event-sink-the-first-arc-owned-occurrence)),
  newest first, bounded to the last 24 hours and capped at 200 rows; each row carries `ts`, the
  `event_type` name it is typed by (on the wire as `key`, e.g. `call-started`) with its
  `event_type_id` beside it, `origin` (caught/caused/derived/scheduled), `instance`,
  `message`, optional `attributes`, `provenance` (`observed` for direct collection), and the `source`
  interface type.
- `GET /components/{name}/reconciliation` pivots want/told/is over the series: per declared
  property, the **want** (the current declared value, with the contract default coalesced in), the
  **told** (the `intended` value a command
  set), and the **is** (the latest `observed` value), all three
  [latest-series-row reads](/architecture/properties/#current-value-is-the-latest-series-row),
  with **drift** (observed present and disagreeing with declared) computed on read.

**Three registries ride the `/property-types` shape** (estate-wide reference data, no scope injection;
official types are read-only, a 409). The **metric_type catalog is the numeric keyspace**:
`GET/POST/PATCH/DELETE /metric-types[/{name}]`, gated `metric_type:read` / `:create` / `:update` /
`:delete`, each type carrying the numeric facts (`unit`, `precision`); the two sample catalogs and
`event_type` share one resolution namespace, so a create is refused when a sibling holds the name
([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)).
The **event_type registry is the occurrence keyspace**:
`GET/POST/PATCH/DELETE /event-types[/{name}]`, gated `event_type:read` / `:create` / `:update` /
`:delete`; an ingested occurrence is typed by a registered `event_type` name (the log-to-event
promotion, ADR-0063), the optional `payload_schema` a JSON Schema fragment for its payload. The
**command_type registry is the do catalog**:
`GET/POST/PATCH/DELETE /command-types[/{name}]`, gated `command_type:read` / `:create` / `:update` /
`:delete`, each type carrying a `settle_window_seconds` and an optional `target_property_type` (the
property a settleable command sets; the metric target arm is storage-deep, its authoring surface
deferred). `POST /components/{name}/commands:issue` (gated `command:issue`,
scope-injected through the component) is the write: it records the invocation, writes a caused event,
and (for a settleable command) opens an intended value, returning the computed settlement verdict
(none/pending/settled/failed).

:::note[Thin cuts today]
The operationally useful slice, not the full CRUD matrix. A **node** carries the full set (create,
list, get, update, delete, plus `:enroll` and `:claim`); its delete **cascades** its interfaces, their
derived tasks, its node-owned tags and self-telemetry, and its enrollment credential. An **interface**
`PATCH` changes only its node placement and its params (target); the type (and the name it derives)
and the owning component are fixed at creation, and a delete is refused while a task still references
it (a **409**). The four built interface types are `icmp`, `tcp`, `ssh`, and `http`; there is no
`interface_type` list route yet.
:::
## Secrets: masked reads, an audited reveal

A **secret** is a typed, encrypted-at-rest operator value ([config, credentials, and
variables](/architecture/variables/)); its routes (`GET /secret-types`, `GET` / `POST /secrets`,
`PATCH` / `DELETE /secrets/{id}`, `POST /secrets/{id}:reveal`; shapes in the
[reference](/reference/api/)) are a worked instance of the conventions above. Secret is a **sensitive
resource**
([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)),
off the viewer `*:read` floor: the registry and directory reads need an explicit `secret:read` grant
(seeded to operator, deploy, admin, and owner), the writes gate on
`secret:create` / `secret:update` / `secret:delete`, and the plaintext decrypt on **`secret:reveal`**,
held by operator and deploy (the device secrets in their scope), admin (via `secret:>`, which alone
reaches the admin-sensitive `:admin` tier), and owner (`>`). Every `:reveal` writes an
[audit](/architecture/audit/) row (verb `reveal`) in the same call. The directory filters each
secret's owner placement against the caller's read scope, admin-sensitive secrets visible only to the
admin tier; every other read masks a secret field (`••••••`), only `:reveal` returns plaintext. A
create names its owner (`owner_kind: platform|location|component`; the system band is retired,
ADR-0052); a `PATCH` re-seals the given `fields`, merged over the stored value so an omitted field
keeps its value. A `platform` secret needs an all-scope grant **and** `platform:create`
(`platform:update` / `platform:delete` on later writes at the tier).

A **variable** is the plaintext sibling of a secret ([config, secrets, and
variables](/architecture/variables/)): the same owner arc and cascade (`owner_kind:
platform|location|system|component`), shown in the clear (no registry, no mask, no reveal), the value
polymorphic JSON typed by `value_type` (`string|int|float|bool|json`) and validated against it on
write. `GET /variables` is the **all-scope admin directory** (like the secret directory, a non-all
scope is a 403) on `variable:read`; `POST /variables` creates (201, `variable:create`),
`PATCH /variables/{id}` replaces the `value` (`variable:update`), and `DELETE /variables/{id}` removes
(204, `variable:delete`, admin and owner), with the same `platform` tier rule as a secret.

A **tag** ([tags](/architecture/tags/)) is a `key: value` label, and its routes split along the
governance line: **minting a tag** is a tenant-wide governance action, **setting a value** is the
owning entity's own write. The tag vocabulary and an entity's tags read on the viewer floor
(`tag:read`, `component:read`).

- `GET /tags` lists the governed tag vocabulary (`{tags: [tag]}`, `tag:read`); a `tag` body is `{id,
  name, applies_to, propagates}`, the `name` on the ordinary entity name rule (lowercase letters,
  digits, and hyphens, at most 100 characters, a uuid-shaped name refused), a 422 otherwise,
  `applies_to` an entity-kind allow-list (empty = universal), `propagates` default true.
  `POST /tags` mints a tag (201), `PATCH /tags/{name}` revises it (the name is fixed), and
  `DELETE /tags/{name}` removes it, cascading its bindings (204): `tag:create` / `tag:update` /
  `tag:delete`, all-scope.
- `POST /tags/{name}:setPlatform` sets the **platform-tier** value for a key from `{value}`;
  `POST /tags/{name}:clearPlatform` removes it (204). A platform binding has no owning entity, so it gates on
  `tag:update` plus `platform:update` (the install-wide tier permission below).
- `GET /{components,systems,locations,nodes}/{name}:listTags` lists the bindings set **directly** on one entity
  (`{tags: [tagBinding]}`, each `{key, value, owner_kind, owner_id?, owner_name?}`, the entity's `:read`).
- `POST /{components,systems,locations,nodes}/{name}:setTag` binds a value from `{key, value}`; the key
  must exist and its `applies_to` must admit the kind (a 422 otherwise). Setting a value is the
  entity's own write, so it gates on the entity's **`:update`** (`component:update` and friends), not a
  tag permission; `POST /{...}/{name}:removeTag` from `{key}` removes the binding (204). Bindings are
  custom methods on the entity rather than a nested collection, so the generated CLI stays
  collision-free.
- `GET /components/{name}/effective-tags` is the **cascade** for one component: each a `resolvedTag`
  (`{key, value, owner_kind, owner_id?, owner_name?, band, depth, winner}`), keys unioning and values
  overriding most-specific-wins, with the winner and shadowed candidates. A non-propagating key resolves
  only from a binding on the component itself (`component:read`, read-scoped).
- The directory list routes (`GET /components`, `/systems`, `/locations`) each carry an **`effective_tags`**
  map (`{key: winning_value}`) on every row, resolved for the whole page in one batched query. A
  component resolves the full arc; a location resolves `platform` plus its location tree; a system
  resolves `platform`, its system tree, and the location it is placed at. Provenance lives in the
  per-entity effective-tags detail, not the row.

The **classification catalogs** ([core entities](/architecture/core-entities/#catalog-reference-data-vendor-driver-component_type))
are official-vs-custom registries the inventory layer references, on the same pattern as the
`*_type` registries; `vendor` and `driver` are flat, `component_type` a hierarchy above `product`
([core entities](/architecture/core-entities/#catalog-reference-data-component_type)), and
`system_type` its system-side counterpart, the coarse taxonomy of what kind of space a system is
([core entities](/architecture/core-entities/#catalog-reference-data-system_type)). Each is its own
resource with one shared CRUD shape: the list routes
(`GET /vendors`, `/drivers`, `/component-types`, `/products`, `/standards`, `/system-types`) order
alphabetically by display name, and each list and per-id `GET` sits on the viewer floor
(`vendor:read` / `driver:read` / `component_type:read` / `product:read` / `standard:read` /
`system_type:read`, which `*:read` carries); `POST` mints a custom
row (201) and `PATCH` updates and `DELETE` removes (204), gated `<resource>:create` /
`<resource>:update` / `<resource>:delete`, all at the admin tier. An **official** row refuses the write
(`PATCH` and `DELETE` both 422), except on a registry that has adopted the **fork**
([ADR-0095](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)):
there a `PATCH` of a shipped row succeeds without writing it, storing the caller's version over it and
answering 200 with `forked: true` under the same id, `DELETE` is still 422 (a fork is an overlay, not
ownership), and `POST /component-types/{id}:restore` discards the fork (409 when there is none) so later
releases reach the row again. `component_type` is the first adopter; `official` and `forked` together
are the row's origin (shipped, yours, or yours overriding shipped). The `{ref}` grammar is unchanged
either way: one uuid and one name per logical row, forked or not, and the namespace never appears in a
URL.

A **nested** registry (`component_type`, `system_type`) returns the flat set with each row's parent
link in both forms, so the console reconstructs the tree client-side; the row carries its **own**
`stem` / `abbrev` / `icon`, unresolved, because inheritance is the resolver's job and a read that
silently filled a blank from an ancestor would make the override invisible. `DELETE` on one is refused
with a 409 while it still parents another node or still classifies a row (`system_type` counts both
sides).

A registry body carries **both handles** like every other name-bearing resource
([above](#shape-resources-and-verb-methods),
[ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
a create takes the `name`, the response carries the minted uuid, and a reference to one (`vendor`,
`driver`, a parent) resolves whichever form it is given.

What each catalog adds over the shared shape (bodies in the [reference](/reference/api/)):

- A **vendor** (Crestron, Biamp, ...) names an organization, generalizing the former manufacturer-only
  `component_make`: a `kind` of `manufacturer` / `integrator` / `developer` (default `manufacturer`, a
  422 otherwise), and a `website` validated to an `http`/`https` scheme on write (a 422 for any other
  scheme, for example `javascript:`).
- A **driver** (Generic SNMP, Cisco xAPI, ...) names the implementation that gets, emits, or sets a
  product's signals, with an optional `version`.
- A **component_type** (Mic, Camera, Wireless Mic, ...) is the device-class genus a product is
  classified under, a **hierarchy** (a subtype falls within its ancestor's subtree): what a
  [system role's typed-slot guard](#roles-a-system-declares-a-slot-a-component-fills-it) accepts.
- A **product** ([core entities](/architecture/core-entities/#catalog-reference-data-product)) is the
  concrete **SKU** that ties the leaf catalogs together and the target of `component.product_id`: a
  vendor, a driver, a `kind` of `device` / `app` / `service` / `vm` (default `device`, a 422
  otherwise), a required `component_type`, and an optional `parent_product_id` variant, the `vendor`
  and `driver` handles reading the referenced registry's current name beside its uuid. An unknown
  vendor / driver / parent / component_type reference is a 422, and a product still referenced by a
  component is refused (409).
- A **standard** ([core entities](/architecture/core-entities/#catalog-reference-data-standard)) is the
  **blueprint a system conforms to** (Huddle Room, Classroom, Auditorium), the system-side counterpart
  of a product. Because it carries its own declared-property contract it is a **Catalog entity, not a
  bare type registry**: its own `standard:*` resource at `/standards`, not `/types/system`, with an
  optional `parent_standard_id` (an unknown parent a 422); a standard still referenced by a system is
  refused (409). The **shipped** standards are `official: false`, so unlike a seeded product they are
  fully editable
  ([the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)).

### The install-wide tier permission

The cascade's least-specific tier is **`platform`** ([cascade](/architecture/cascade/)), and a write that
lands there needs **two** permissions: the resource's own (`secret:create`, `variable:update`,
`tag:update`, `settings:update`) **and** `platform:<action>`, because estate **scope** and install-wide
**authority** are different questions
([identity and access](/architecture/identity-access/#install-wide-authority-is-not-estate-scope)).
`platform:*` is seeded to `admin` (and to `owner` through `>`); `operator` and `deploy` do not hold it.
The tier gate is **published in the spec** like every primary gate: an
`x-omniglass-platform-permission` extension beside the route's `x-omniglass-permission` stamp, both in
the route-derived permission universe. Where the request body names the tier (`owner_kind: platform`,
and every settings write) the handler checks it up front; where only the stored row knows its tier (an
update or delete by id) the resolved capability rides into the Gateway alongside the ABAC scope, so the
404-versus-403 split stays non-disclosing.

A second permission can also be **conditional on the request** rather than on a tier, and it is stamped
the same way, as `x-omniglass-conditional-permission`. There is one today: `POST /components` accepts a
`system`, and what it does with it is insert that component's **primary membership**, the same row `PUT
/systems/{name}/members/{component}` writes under `system:update`. Two routes writing one row must cost
the same permission, or the cheaper one is the way around the other, so a create that names a system
requires `system:update` and resolves it in that scope
([ADR-0107](/architecture/decisions/#adr-0107-a-create-that-writes-a-membership-costs-what-the-membership-route-costs)).
The check is in the handler rather than the middleware because the condition is a body field and
middleware cannot see the body; the stamp is what keeps the spec a faithful map of what the route
enforces.

## Properties: a classifier declares, an instance sets

A **contract** is the set of properties a classifier's instances expose
([core entities](/architecture/core-entities/#declared-properties-the-classifier-contracts-and-the-declared-rows)).
Each contract is a **sub-collection of its classifier**, addressed by property name, so the line is
idempotent: `PUT` declares it or revises it in place. Type and validation come from the
[property catalog](/guides/admin/properties/), not the body. Three classifiers carry a contract, on
identical route shapes:

- `GET /products/{id}/properties`, `PUT` / `DELETE /products/{id}/properties/{property}`, gated
  `product:read` / `:update` / `:delete`; `GET /standards/{id}/properties`, `PUT` /
  `DELETE /standards/{id}/properties/{property}`, gated `standard:read` / `:update` / `:delete`.
- `GET /location-types/{id}/properties`, `PUT` / `DELETE /location-types/{id}/properties/{property}`,
  gated `location_type:read` / `:update` / `:delete`, the registry's own resource.

The list returns the contract ordered by property name, each line
`{property_type_name, property_type_id, default_value, required}`: the label and type are the catalog's
to serve, so a surface that wants them reads `/property-types` alongside. `PUT` takes
`{default_value?, required?}`; `DELETE` withdraws the line (204), and instances **keep** any value they
set for it, now off contract. An **official** classifier refuses the write (422), an unknown
classifier is a 404, and a property the catalog does not know is a 422.

An instance's **values** are the other side of the contract, and unlike the classifier routes they are
**ABAC-scoped through the instance**, so an out-of-read-scope instance is a non-disclosing **404** and
every write is audited. Three owners are addressable
(`GET /{components,systems,locations}/{name}/properties`, `PUT` / `DELETE .../{property}`), each gated
by its own entity's permission (`component:read` / `component:update` and friends): the catalog governs
the vocabulary, the owning entity its values, the same rule tag bindings follow.

The `GET` is the **effective read**: every property the instance's classifier declares (its `product`,
its `standard`, its `location_type`), resolved to `coalesce(the instance's own value, the contract
default)`, plus every property set directly on the instance off contract, each row carrying the
catalog's display fields plus the value, the default, `is_set` (the override marker), `from_contract`,
`required`, and the `value_id` the surface clears. An instance with **no classifier** returns only its
off-contract values. `PUT .../{property}` sets the instance's value from `{value}`, idempotently; the
property need **not** be on the contract, but must exist in the catalog (422 otherwise).
`DELETE .../{property}` clears it (204), falling back to the contract default or leaving the effective
read entirely when off contract; clearing a value the instance never set is a 404.

## Roles: a system declares a slot, a component fills it

A **[system role](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled)** is a slot a
system needs filled, in three arcs: **declaration** (what a standard says every conforming system
needs, and what one system declares ad-hoc), **resolution** (the per-system read merging both with who
fills each role today), and **staffing** (assign and unassign). It is **not** the
[IAM role](/architecture/identity-access/): `/roles` is the RBAC catalog, these routes are the estate model.

A role is addressed **by name within its owner**, so every declaration is a `PATCH` that declares or
revises in place. The body is `{update_mask?, display_name?, quorum?, capacity?, position_labels?,
accepted_types?, pinned_products?, impact?, alternate?}`, patched under the
[write mask](#partial-responses-field-masks): the fields the body populates change and the rest of
the declaration is left alone, and `update_mask` names the fields that change whether the body
populates them or not. `accepted_types`, `pinned_products` and `position_labels` each **replace**
their set wholesale when written, so clearing one means naming it in `update_mask` (an empty list is
not a populated field, so on its own it means unchanged). `capacity` is the most components the role
will accept (at least `quorum`), unbounded on first declare, and naming `capacity` in `update_mask`
with no value is what clears it back to unbounded. `impact` is `outage` / `degraded` / `none`
(`degraded` on first declare), what an impaired role does to its system's
[health](#health-the-verdict-and-why); an unknown impact is a 422. `alternate` is the
`choice-name/alternate-name` this role joins, an empty string detaching it; every role read returns it
in the same form. Gating follows the owner:

- `GET /standards/{id}/roles` plus `PATCH` / `DELETE /standards/{id}/roles/{role}`, gated `standard:read` /
  `:update` / `:delete`. Withdrawing a role takes every assignment conforming systems made to it (a
  cascade); a role the standard does not declare is a 404.
- `PATCH` / `DELETE /systems/{name}/roles/{role}`, gated `system:update`, for a role declared **directly on
  one system**. A role the system does not declare **itself** is a 404 here: an inherited role is
  withdrawn on the standard.
- `GET /systems/{name}/roles` is the **resolved read**, gated `system:read`: the declaration
  (including `impact`, `capacity`, `position_labels`, `alternate`) plus `from_standard`, `assigned_to` (the
  component names filling it here, in position order), `positions` (each entry's own 1-based
  position, index for index with `assigned_to`; not assumed dense, since an unassign leaves a gap
  rather than compacting), `assigned`, and **`understaffed`** (how many more before quorum), the
  counts **served, not computed by the client**. A one-off system returns only its own roles.
- `PUT /systems/{name}/roles/{role}/assignments/{component}` puts a component in the role (204,
  idempotent); `DELETE` takes it out (204; a component not filling the role is a 404). Both gate on
  `system:update`.
- `POST /systems/{name}/roles/{role}:swapPositions` exchanges two occupants' positions, from
  `{position, with}` (204, `system:update`): the only reorder primitive the API exposes, since a
  position is an ordering attribute of an assignment, not a second address for one, and there is no
  "move to index N" route.

Every system route resolves its owner **within the caller's scope first**, so an
out-of-scope system is a **non-disclosing 404** on read and write alike, and every write is
audited in the same transaction.

**A refusal names both parties, and its status follows what it depends on.** A component fills a
slot only when its product's `component_type` falls within a type the role's `accepted_types` names
(self or a descendant, any type if empty), and, if the role pins products, only when its product is
one of them:

```
component "panel-1" is a display; role "table-mic" wants a video-bar
```

That guard, and a bad declaration (an unknown type, product, or impact), are **422**: the request is
invalid on its own, regardless of anything else in the estate. A component already staffing a
**different** role in the same system, or an assignment a role's declared `capacity` cannot hold, are
**409** instead:

```
component "bar-1" already fills "main-display" in "boardroom-a"; a component fills at most one role per system
```

the request is fine in isolation, it conflicts with other rows (a component fills at most one role
per system; lowering a capacity below the count already assigned is refused the same way). Both are
the **semantic**-refusal case in the [status table](#errors-one-problemjson-envelope), not an
authorization one: the message tells the operator the next move. Around it: an unknown role is a
**404**, and an unknown (or out-of-scope) system or component the same non-disclosing **404**. The
guard runs once, at assignment; afterward an occupant keeps its slot unless its own
[health](#health-the-verdict-and-why) verdict goes to outage (a lesser alarm degrades it but does not
cost it the slot).

## Health: the verdict, and why

**[Health](/architecture/health/)** is two shapes on this surface: the **alarm** (what is wrong with one
component) and the **report** (what that means for a system or a location); an alarm is
component-local, reaching a room only through the component's own verdict. Only a `critical` alarm
takes that verdict to outage and impairs every role the component occupies while it is active; an
`info` or `warning` alarm degrades the component but leaves it occupying its roles.

An alarm hangs off its component and rides that component's gating:

- `GET /components/{name}/alarms` lists them newest first (`component:read`), the **active** set by
  default, the whole history with `include_cleared`, and the queue an operator actually works with
  `unacknowledged` (on its own, raised and unacknowledged; with `include_cleared`, also the
  incidents that came and went unattended).
- `POST /components/{name}/alarms` raises one from `{severity, message?, dedup_key?}` (201,
  `component:update`). `severity` is `info` / `warning` / `critical`, driving the component's own
  verdict (any active alarm degrades it, a critical one is an outage); `dedup_key` is the condition
  identity, defaulting to the message. A bad severity is a **422**.
- `DELETE /components/{name}/alarms/{id}` clears it (204, `component:update`). The row is **kept**, so
  the record of what was wrong outlives the fix; clearing one already cleared, or another
  component's, is a **404**.

Raising and clearing both **recompute health in the same transaction**, so an alarm and the verdict it
caused are never separately visible, and the recorded edge carries the time the estate changed.

**Acknowledgement is the one alarm write that does not**, and it is the one that does not ride
`component:update` either:

- `POST /components/{name}/alarms/{id}:acknowledge` records that a human has seen it (200,
  **`alarm:acknowledge`**). It changes nothing else: `cleared_at` is untouched, no health is
  recomputed, and the component stays exactly as broken as it was. Acknowledging is not fixing.

The permission is its own because recording that somebody looked is not editing the component, and a
role may hold one without the other. Its scope resolves on the **component tier from
`alarm:acknowledge` itself**, so an estate-wide read never widens what may be acknowledged and a
narrow component write never narrows it; a component outside that scope is a non-disclosing **404**.
Acknowledgement is **orthogonal to cleared** in both directions: an alarm can be acknowledged and
still raised, raised and unacknowledged, or cleared having never been acknowledged, and a cleared
alarm can still be acknowledged by whoever reviews the history. Acknowledging twice is **idempotent**:
the first person and the first time are what the row keeps, and the no-op writes no second audit row.
Un-acknowledging is deliberately not a verb yet
([#728](https://github.com/hyperscaleav/omniglass/issues/728)); snooze and resolve are
[out of scope with reasons](/architecture/alarms-actions/).

The reports are one shape over two owners:

- `GET /systems/{name}/health` (`system:read`) returns the verdict (`healthy` / `degraded` /
  `outage`) plus `roles`: every role the system needs filled, where `satisfying` counts the assigned
  components currently occupying it (their own verdict is not outage; a degraded one still counts),
  `down` names the **assigned** components whose own verdict is outage, and `alarms` the active
  alarms on those. An impaired role with an **empty** `down` is **short-staffed**, not broken, a
  different job for the operator.
- `GET /locations/{name}/health` (`location:read`) returns the same envelope with `systems` filled
  instead: every system placed **anywhere** beneath the location, with its verdict, as the drill-down.
- `transitions` is the **recorded edges** over the last 30 days, oldest first, each `{ts, verdict}`: one
  entry per change, never a sample.

Both resolve their owner **within the caller's scope first** (an out-of-scope system or location is a
**non-disclosing 404**), and **neither read writes anything**: the verdict served is computed from the
very rows served beside it, so a report can never disagree with its own evidence
([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)).

## Files: content-addressed bytes behind a handle

A **file** is a searchable handle over a content-addressed [blob](/architecture/files/): the metadata is
tenant-wide (no placement arc), so these routes take **no scope**, only the `file:<action>` permission
plus the per-file `sensitive` tier. Reading rides the **viewer floor** (`file:read`, which `*:read`
carries); a **sensitive** file is fenced to the `:admin` tier (`file:read:admin`), hidden from a lister
and a **non-disclosing 404** to a reader without it, exactly the
[secret sensitivity rule](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier).
The bytes ride **base64 in JSON** on both create and download (the
[avatar precedent](/architecture/decisions/#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)),
so the whole surface stays under the authz middleware and generates a uniform client.

`GET /files` is the directory (`{files: [file]}`), sensitive files omitted below the admin tier;
`GET /files/{id}` returns one handle's metadata; `GET /files/{id}:download` returns
`{name, content_type, content (base64)}`, the blob read back and its hash verified (all `file:read`).
`POST /files` creates one from `{name, content_type, content (base64), sensitive?}` (201,
`file:create`): the server hashes the bytes, **deduplicates** the blob, and writes the handle
(`sensitive: true` additionally needs the admin tier). `DELETE /files/{id}` removes the handle (204,
`file:delete`); the blob is freed in the same transaction when no other handle references it. A `file`
body is `{id, name, content_type, size, sha256, sensitive, created_at}`; the `sha256` is the content
address.

:::design[Views and the SSE live relay, tracked in #523]
## Reads beyond one resource are views

A single resource reads through its typed `GET`. Anything richer (a dashboard, an explorer, the cascade
"why did this value win" view) goes through a **[view](/architecture/views/)**: a named query returning a
uniform `ViewResult` (`{columns, rows}`), bound by declared params at `/views/{id}:run`, executed through
the same scoped gateway; an operator never gets raw SQL. A **live** read may upgrade from polling `:run`
to a **server-relayed [SSE](/architecture/messaging/) stream** over the same seam: the subscribe is
**capability fast-rejected** at open (not authorized there), then the server holds the internal
subscription, re-runs the gateway scope per message against each message's owner, and pushes only
visible deltas. The operator never connects to the bus, so the live path adds no second authorization
model.
:::

## Versioning and evolution

The path carries the major version (`/api/v1`). Within a version, change is **additive only**: new
fields, new optional params, new resources, never a removal or a meaning change; a breaking change is a
new major version. Because the [OpenAPI 3.1 document is generated](/contributing/api-first/) from the Go
structs and the clients from that, the contract cannot drift from the implementation: a drift check fails
the PR on an unregenerated route change.

:::design[The MCP surface, tracked in #522]
## Also an MCP surface

The same OpenAPI document that generates the typed SPA client and the CLI also generates an **MCP
server**, one more [generated client](/contributing/api-first/) over the same gateway: every tool call is
the same route permission, the same gateway scope, the same same-transaction
[audit](/architecture/audit/), **not a side channel**. The **tool catalog is curated, not a raw
one-method-per-tool dump**: task-oriented tools, the [views](/architecture/views/) exposed as search and
query tools, errors shaped for a model. The MCP server runs under the **authenticated `human` or
`service` principal's** credential, so its reach is exactly that principal's grants
([AI](/architecture/ai/)).
:::

:::design[The full node NATS contract, per ADR-0036]
## The node path is the NATS contract

Nodes do **not** speak HTTP. The edge is a NATS client over the WAN: telemetry to a JetStream stream,
commands from a durable server-side JetStream queue, enrollment by NATS JWT/nkey, all on the sibling
**NATS subject contract**, not this page's routes. The old node HTTP custom methods are gone; the proto
definitions survive **as the NATS message schema**. The contract lives in
[messaging](/architecture/messaging/) and on the [node](/architecture/nodes/) page; the same AIP
spirit, error envelope, and idempotency carry across.
:::

## Self-describing

The running server serves `GET /api/v1/openapi.json`, `/openapi.yaml`, and a human reference page, so the
public contract is discoverable live against any deployment; the internal NATS subject contract
(subjects, message schemas, stream and consumer definitions) is published from the running server the
same way, the sibling of OpenAPI for the bus.

Related: [API first](/contributing/api-first/), [messaging](/architecture/messaging/),
[identity and access](/architecture/identity-access/), [audit](/architecture/audit/),
[UI](/architecture/ui/) (the views BFF and the renderer contract), and
[expressions](/architecture/expressions/) (the `filter` language).
