---
title: API
description: "The API contract: AIP-style resources and :verb methods, cursor lists, a problem+json error envelope, idempotent writes, and long-running operations carried by the action row."
sidebar:
  badge:
    text: Design
    variant: caution
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

## Shape: resources and `:verb` methods

Everything lives under `/api/v1`. The path shape is derivable, not special-cased:

- **Plural resource collections**, standard methods by primary key (AIP-style): `POST` creates (409 on
  PK collision), `GET` reads, `PATCH` partial-updates (AIP-134), `DELETE` removes. No upsert shortcuts.
- **Custom methods carry a colon**, `:verb` not `/verb`, for anything that is not CRUD:
  `/alarms/{id}:ack`, `/components/{name}:apply`, `/views/{id}:run`. The verb
  is also the **permission**: `:ack` is gated by `alarm:ack`, so the route and the
  [authorization](/architecture/identity-access/) check share one vocabulary.
- **Singular kind sub-segments** for the typed families: `/rules/calc`, `/datapoints/metric`,
  `/types/component`.

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
  invariants, and the API is the gateway's only caller, so there is no unscoped path.

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
