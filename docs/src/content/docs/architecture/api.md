---
title: API
description: "How the API is built: Huma operations that generate the OpenAPI document and every client, AIP-style resources and :verb methods, one error shape, and a permission stamped on every route."
sidebar:
  order: 50
  badge:
    text: Partial
    variant: note
---

The API is the one way into Omniglass. The console, the CLI, the [MCP server](#also-an-mcp-surface),
scripts and integrations all call the same HTTP operations under `/api/v1`, and those operations are
the only caller of the [Storage Gateway](/architecture/storage/). Outposts are the one exception: past
their first claim they speak the NATS contract instead ([below](#the-outpost-path-is-the-nats-contract)).

Every route, its parameters and its body are in the [API reference](/reference/api/), rendered from
the OpenAPI document, so this page carries none of them. It is the conventions every route follows.

## One source, generated outward

An operation is a Go struct registered with [Huma](https://huma.rocks/) on a chi router
(`internal/api`). `make gen` dumps the OpenAPI 3.1 document from those registrations
(`api/openapi.json` and `api/openapi.yaml`) and generates everything else from it: the typed console
client (`web/src/api/api.gen.ts`), the cobra CLI (`internal/cli`), the MCP tool catalog
(`internal/mcp`), and the reference pages. The live server and the spec dump register through the one
function, so the routed surface and the document cannot disagree, and CI fails a PR whose generated
files are stale. The doctrine and the generator table are [API first](/contributing/api-first/).

A running server also serves its own document at `/api/v1/openapi.json` and `/api/v1/openapi.yaml`,
so a client can discover the contract of the version it is talking to.

## Shape: resources and `:verb` methods

Paths are derivable, not special-cased
([ADR-0060](/architecture/decisions/#adr-0060-a-resource-is-one-kebab-case-noun-nesting-means-ownership)):

- **A resource is one plural kebab-case noun**, and nesting means ownership: `/components/{name}/alarms`
  are a component's alarms. Each catalog is its own collection (`/metric-types`, `/event-types`).
- **Standard methods** are `POST` to create, `GET` to read, `PATCH` to update part of a row, `DELETE`
  to remove. There is no upsert.
- **Anything else is a custom method with a colon**, `:verb` rather than `/verb`:
  `/components/{name}/commands:issue`, `/secrets/{secret}:reveal`, `/outposts:claim`. On a member it
  acts on that row; on the collection (`/systems:checkName`, `/components:batchCreate`) it acts on the
  set.
- **The verb is the permission.** `:issue` is gated by `command:issue`, so a route and its
  [authorization](/architecture/identity-access/) share one vocabulary.
- **A rename and a move are their own verbs**, never `PATCH` fields. `:rename` breaks every reference
  someone stored outside Omniglass, and `:move` changes what scope a row falls under, so each is an act
  a grant can withhold on its own
  ([ADR-0088](/architecture/decisions/#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)).
- **`/auth/me` is the self-service family**: it resolves its target from the session, never a path
  id, so it needs no permission beyond being signed in. Its admin counterparts on `/users/{id}` do.

### Every operator resource answers to its name

A row with a name carries two handles: a uuid `id`, which every foreign key stores, and a renameable
`name`, which an operator reads and types. A create takes the name, the database mints the uuid, and
every write returns it, so a client that must never drift stores the uuid
([ADR-0076](/architecture/decisions/#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)).

Wherever a uuid is accepted, so is the name
([ADR-1133](/architecture/decisions/#adr-1133-every-operator-resource-answers-to-its-name-and-an-endpoint-is-its-component-and-its-name)):

| Resource | Accepts beside the uuid | Example |
|---|---|---|
| location, system, component | a bare name, or a dotted address | `boi.17c.415a.$comp.display-1` |
| endpoint | `<component>/<endpoint name>` | `boi.17c.415a.$comp.display-1/https` |
| secret, variable | a bare name, or `<owner>/<name>` (`$platform` for the install) | `$platform/demo-panel-login` |
| file | its filename | `codec-firmware-2.1.4.txt` |
| user group | its name | `night-shift` |
| user | a username or a machine user's name | `alice` |

The slash in an endpoint, secret or variable reference is one path segment, so a URL percent-encodes
it; the generated CLI does that for you. A dotted address is a positional lookup resolved by structure
([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)).

**Scope decides before ambiguity does.** A name is matched only against the rows the caller may read,
so a row out of scope never makes an in-scope name ambiguous, and an absent row and a hidden one give
the same 404. A name more than one visible row answers to is a 409 that lists each candidate in a
spelling that does resolve. Resolution runs inside the request's transaction and yields the uuid; the
permission check and the audit row then run on that row exactly as for a uuid.

Rows without a name stay uuid-only: an alarm, an audit row, a grant, a session or token, and a task
(whose id is its content hash).

## Lists: filter, order, page

Every list runs through the scoped gateway, so it never returns a row outside the caller's visible
set, and any count is over visible rows only. Lists take their own typed filters today (a severity, a
location subtree, `include_cleared`). The two that can grow without bound page: `GET /alarms` with an
opaque `page_token` and `next_page_token`, and `GET /audit-log` backward with `before` and `limit`.

:::design[The AIP list parameters and the read mask, tracked in #522 and #1011]
Every list takes `filter`, `order_by`, `page_size` and `page_token`, and every read takes a `fields`
mask. Pages are cursors, never offsets, so a concurrent insert neither skips nor repeats a row. The
`filter` is one [Omniglass expression](/architecture/expressions/) over the resource's fields, and every
field any of the three names resolves through the gateway's allow-list, with values bound as
parameters, so none of them can inject SQL.
:::

## Writes: the update mask

A `PATCH` writes the fields its body populated and leaves the rest alone. To clear a field that has no
empty value of its own (an integer, a list, a nullable object), the body names it in `update_mask`,
AIP-134 exactly ([ADR-0091](/architecture/decisions/#adr-0091-an-update_mask-says-which-fields-a-patch-writes)):

- **No mask**: the implied mask, the fields the body populated.
- **A mask**: exactly the named fields change, so one the body leaves empty is cleared.
- **`["*"]`**: full replacement, every patchable field written.
- **A field the route does not patch**: a 422 naming it, never a silent no-op.

The mask rides in the body, top-level names only, resolved by one primitive (`internal/updatemask`).
A nullable string keeps the older three-state convention instead: omitted leaves it, `""` clears it, a
value sets it. A nullable object always clears through the mask, since `{}` is a value and a decoded
`null` looks like an omitted key
([ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)).

## Errors: one problem+json shape

Every error is RFC 9457 `application/problem+json` in Huma's stock shape: `title`, `status`, `detail`,
and for validation an `errors` array of `{location, message, value}`
([ADR-0068](/architecture/decisions/#adr-0068-the-api-error-model-is-the-stock-rfc-9457-shape)).

| Status | Meaning |
|---|---|
| 400 | a malformed request |
| 401 | not signed in |
| 403 | the caller lacks the permission, or can read the target but may not do this to it |
| 404 | not found, including out of the caller's read scope |
| 409 | a conflict: a name already taken in its bucket, an ambiguous name, a stale or racing write |
| 422 | well-formed but invalid, naming the field or the rule |
| 429 | too many at once (open watches, waiting commands) |
| 503 | a dependency is not there to take it (no telemetry stream, no assistant model) |

The 403 and 404 split by where the target sits in the caller's
[scope](/architecture/identity-access/): a target the caller cannot read is a 404, so the API never
confirms a row exists outside what the caller can see; a target it can read but not act on is a 403,
which leaks nothing it could not already read.

## Every route is stamped and scoped

Each operation declares its permission at registration, and the stamp lands in the document as
`x-omniglass-permission`. A route whose write lands on the install-wide tier carries a second,
`x-omniglass-platform-permission`; one whose second check depends on the body (a component create that
names a system also writes a membership) carries `x-omniglass-conditional-permission`
([ADR-0107](/architecture/decisions/#adr-0107-a-create-that-writes-a-membership-costs-what-the-membership-route-costs)).
So the document is a machine-readable map of the authorization contract, and the set of stamps is the
[permission universe](/architecture/identity-access/#the-permission-universe-published-per-route) the
Roles page shows.

Every query carries the caller's scope, injected by the gateway, and every write records its
[audit](/architecture/audit/) row in the same transaction as the change. Both are
[identity and access](/architecture/identity-access/) invariants, not conventions a handler opts into.

## Long-running work: the command is the handle

Issuing a command to a device does not hold the request open for the device. `:issue` answers the
command row at once, its `phase` saying where it stands (queued, delivered, awaiting a reply or an
observation, then settled, failed or timed out), and `GET /components/{name}/commands/{id}` follows
it. A caller that would rather wait passes `wait` and a `wait_timeout` of at most 120 seconds, and is
answered with the row as it stands when it settles or the wait runs out
([commands](/architecture/commands/)).

:::design[Idempotency keys, optimistic concurrency, and one handle for every long operation, tracked in #522]
- **`Idempotency-Key`** on a `POST` or a state-changing custom method: a retry with the same key
  returns the original success rather than doing it twice. Only a success is remembered, and a replay
  runs authorization again first, so a revoked grant is never bypassed by a replay.
- **Optimistic concurrency**: a conditional update carries the row's version, and a write against a
  stale one is a 409 rather than a silent last writer wins.
- **Every long operation returns a handle** the way a command does, so a reconcile or a rotation is
  followed the same way.
:::

## Reads shaped for a surface

A typed `GET` answers "what is this row". A few reads answer "what does this screen need" instead,
where composing the row reads in the browser would be the wrong shape: `GET /views/fleet` (every
location, system and a dot per component, for Explore), `GET /views/platform` (for Platform health),
and `GET /search` (the command palette). Each is read-only, resolves its own scope per tier, and serves
the values the platform already recorded, so two surfaces cannot disagree about one room.

A read nobody could write a route for in advance (a dashboard widget pointed at anything, an agent's
question) goes through a [view binding](/architecture/views/) instead: `POST /views:run` for history
and `GET /views:watch` for the live tail, the one scoped read of the sample store.

The same stream is how the console stays current without polling
([ADR-2731](/architecture/decisions/#adr-2731-the-console-follows-state-over-its-one-live-stream-and-polling-is-guarded-out)):
beside its bindings a watch names `topics` (`system:<ref>`, `component:<ref>`), and a `changed` event
says a write moved one. A notice is an invalidation, not the data: the client refetches the route it
already reads, so authorization, redaction and the answer's shape stay on that route. Each topic
resolves in the caller's scope when the stream opens, an out-of-scope one the same 404 as a missing
one, and a stream names a topic at most once each 250 ms.

Every Server-Sent Events route keeps the same hygiene: it sends `X-Accel-Buffering: no` so a proxy
does not hold its events, and sets a 10 second write deadline before each send, so a client that stops
reading ends its stream rather than holding a goroutine. A resumable stream (the watch) also sends
`retry:` and, when the server shuts down, ends with a final `mark` carrying its position.

## Health, and why

A health read serves its reasons beside its value. `GET /systems/{name}/health` answers the system's
health with every slot it needs, the members down in each and the alarms on them; a location's names
each system beneath it; each carries the recorded transitions in its range. The value is computed from
the rows served beside it and no read writes anything, so a report can never disagree with its own
evidence. Raising or clearing an alarm recomputes health in the same transaction as the write
([health](/architecture/health/),
[ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)).

## Versioning and evolution

The path carries the major version. Within `/api/v1` change is additive only: a new field, a new
optional parameter, a new resource, never a removal or a change of meaning. A change to a released
route that cannot be additive is a breaking change, called out in its release notes.

## Also an MCP surface

The same document generates the MCP server, one more client over the same routes
([ADR-1162](/architecture/decisions/#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token)).
An operation becomes a tool when its registration carries the `x-omniglass-mcp` stamp; `cmd/mcpgen`
turns every stamped operation into one, listed in the [MCP tools reference](/reference/mcp/). It is a
chosen catalog, not one tool per route, and never a hand-kept list.

The server is at `/mcp` over streamable HTTP, and over stdio through `omniglass mcp`. It admits only a
bearer token the API accepts and dispatches every tool call back through the router with it, so a
call takes the route's permission, scope and audit exactly as a direct call does, its audit row marked
with the `mcp` channel. Each tool carries the hints its method and gate imply (read-only, destructive,
idempotent) so a client can hold a write for its person's confirmation, and its agent class, which an
install's [agent policy](/developer/agents/#what-an-agent-may-do) may refuse outright.

The catalog is **credential-blind**
([ADR-2025](/architecture/decisions/#adr-2025-the-mcp-catalog-is-credential-blind-and-reads-the-fleets-alarms-in-one-call)):
an operation whose request or answer carries a credential in plaintext is stamped
`x-omniglass-plaintext`, and the generator never makes a tool of it. Beside the tools the server serves
embedded **skills** as MCP prompts, workflows such as triaging an alarm, listed in the
[MCP skills reference](/reference/mcp/skills/)
([ADR-2026](/architecture/decisions/#adr-2026-the-mcp-server-serves-embedded-skills-as-prompts)). How
an agent signs in and what it may do: [connect an agent](/developer/agents/) and [AI](/architecture/ai/).

:::design[The fleet query tool over the binding, tracked in #2050]
An agent's fleet query tool, and the analytics skill over it, call the view binding with no read path
of their own. Today the binding's generated tool, `run-views`, is the only way in.
:::

## The outpost path is the NATS contract

An outpost calls the API once, to claim its enrollment (`POST /outposts:claim`), and is answered with
where to reach the bus. Everything after that, telemetry up and commands down, rides NATS subjects on
JetStream, typed and versioned like this document: see [messaging](/architecture/messaging/) and
[outposts](/architecture/outposts/).
