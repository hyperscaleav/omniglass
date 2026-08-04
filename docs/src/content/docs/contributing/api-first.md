---
title: API first
description: The Go API is the single integration contract; the SPA, CLI, and YAML tooling are generated clients of it.
---

The Go HTTP API is the **single integration contract**. The SPA, the CLI, the node
worklist, and the YAML authoring tooling are all **generated clients** of it. Nothing but
the API talks to the database, and the API is described by one machine-readable spec that
cannot drift from the implementation.

## The source of truth is the Go API

Request/response types are Go structs (Huma). The OpenAPI 3.1 document is *generated* from
them, server-less, and committed. Everything downstream is generated from that document.
This is the rule: **you change a Go route or shape, you regenerate, you commit the derived
artifacts.** The `gen-drift` workflow (`.github/workflows/gen-drift.yml`) enforces it in CI,
failing the PR on any diff in the committed generated artifacts.

## The generation pipeline

| Generator | Input | Output | Consumer |
|---|---|---|---|
| `cmd/openapigen` | Huma Go structs | `api/openapi.json` (+ `.yaml`) | everything below |
| `cd web && npm run gen:api` | `openapi.json` | `web/src/api/schema.gen.ts` | typed `openapi-fetch` SPA client |
| `cmd/cligen` | `openapi.json` | `internal/cli/api_gen.go` (cobra) | the CLI, patched via `api_hooks.go` |
| `cmd/docsgen` | the live cobra tree | `docs/src/content/docs/reference/cli/index.md` | the published CLI reference |
| `cmd/erdgen` | the embedded migrations, applied to a throwaway Postgres and introspected | the D2 region of `docs/src/content/docs/architecture/data-model.md` and the schema facts `docs/src/generated/schema.json` | the published ERD and every docs storage table |
| `cmd/seedgen` | the twelve embedded seed YAMLs, parsed in-process (no database) | `docs/src/generated/seed.json`, roles carrying effective permissions | the docs' shipped-set claims |
| `cmd/configgen` | the declarative env registry in `internal/config` | `docs/src/generated/config.json` | the deployment guides' env tables |
| `gen-proto` | `proto/og/v1/*.proto` | committed `*.pb.go` | the NATS `TelemetryBatch` telemetry message |

Two more stages are planned but have **no generator yet**: an MCP tool catalog for AI agents
over the [API contract](/architecture/api/), and a JSONSchema for YAML editor validation. They
land with their consuming slices.

One command runs them all (`make gen`); two focused targets regenerate a subset
(`make gen-proto` for the protobuf wire, `make gen-web` for the spec plus the typed SPA
client). The committed `*.pb.go` lets a contributor build without protoc or a running
server.

## A name is the address, a uuid is identity

**Every response carries both forms of a reference: the name an operator reads and the id it
resolves to.** `{"parent": "rack", "parent_id": "0198f..."}`. The name is what a human types
and what a body round-trips; the id is the stable handle that survives a rename.

**The entity-key rule is enforced on every key-bearing table.** An entity's key is the identifier an
operator types and an address carries (the `rm215a` in `boi.17c.rm215a`), and the rule is
`^[a-z0-9][a-z0-9-]*$` with a 100 character ceiling and the uuid shape refused. It lives in the
contract, not just below it: the create body carries `pattern` and `maxLength`, so the generated
OpenAPI, the typed client, the CLI, and the YAML JSONSchema all enforce it, and the Storage Gateway
enforces it again for callers that never touch a route.

**Every exception is named, in code.** `internal/storage/identity_shape_test.go` declares which of
the four identity shapes each table has, and fails the build on any table that has none or more than
one. It reads the generated schema, so a new table is a failing test until somebody classifies it.
The four shapes, and the exceptions worth stating out loud:

| shape | meaning | the exceptions |
|---|---|---|
| key-bearing | an operator types its key, on the entity-key rule | |
| keyspace | an operator types its key, on `internal/key`'s rule | `property_type`, `event_type`, `command_type`, `tag`, `variable`, `secret` |
| a human identifier that is not a key | looks key-shaped, must never take the key rule | `human` (a username), `file` (a filename), `task` and `blob` (content-addressed) |
| id only | nobody names it, so it is addressed by uuid | every join and telemetry row |

An earlier version of that guard only inspected tables carrying a `name` column, which made it blind
to 28 of the 51. Absence of a `name` is not evidence of absence of an identifier: a username and a
content hash are both identifiers, and both escaped.

A table on the **keyspace** rule (`internal/key`, snake_case with an optional dot hierarchy) is
deliberately outside this one, because
`icmp.rtt_avg` is a legitimate keyspace key and an illegal entity key. Which table is which is not written down
here, because a hand-copied list is drift waiting to happen: `TestEveryNamedTableIsClassified` reads
the generated schema, finds every table carrying a `name`, and fails until each one is classified onto
one rule or the other with its reason. A new table joins the guard by existing.

The exclusions are load-bearing rather than tidy. Barring `.` keeps one key from splitting into two
segments. Barring `*` and `>` keeps a key from reading as a NATS subject pattern. Barring
`$` is what lets an address use sigil accessors without reserving any word, so a location may still
legitimately take the key `sys`.

The test is a **round trip**: a response body can be fed back to the write that produced it
(create a component with `{"parent": "rack"}`, read it back as `{"parent": "rack"}`). When that
fails, every client has to fetch a second collection and join by uuid to render one label, each
slightly differently.

One exception, narrow: **an entity with no name** is legitimately addressed by id, an interface
(its name is unique only within its component), a stored property value, an audit row, a grant, a
principal. A **registry** used to be a second exception, a slug-keyed catalog whose id *was* its
name (`product_id: "cisco-room-bar"`); that is gone. Every registry now has a uuid primary key and
a renameable `name` ([ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)),
so it obeys the rule like any estate entity.

**Every foreign key stores the target's primary key**, a uuid, with no exception: a rename then
has nothing to rewrite, because nothing points at the friendly name. A `_id` column holding a
name, kept alive by `on update cascade`, is the shape this rule exists to prevent; that machinery
is now retired everywhere, including the last place it lived, the registries.

**A path or a join field accepts either form.** `GET /components/{ref}` and a body's
`{"parent": "..."}` both take a uuid or a name; the uuid is tried first, so an id never
collides with a name.

`TestReferencesCarryBothForms` enforces this over the generated OpenAPI in both directions, so a body
cannot silently reintroduce a uuid-only reference (a `*_id` with no name) nor a name-only registry
reference. Its exempt list is the whole of the remaining exception (the nameless entities and a
still-slug-keyed taxonomy), and adding to it is a decision: if the target has a name, carry the
name.

## Conventions (AIP-style)

These are the conventions a route follows while you write it; the complete [API
contract](/architecture/api/) (the error envelope, idempotency, long-running operations,
versioning, and the authorization status mapping) is the architecture of record.

Every operation lives under `/api/v1/*`. The path shape is derivable, not special-cased:

- **Plural collections**, standard CRUD by primary key: `POST` creates (409 on PK
  collision), `GET` reads, `PATCH` updates by PK (AIP-134, partial), `DELETE` removes.
  No upsert/register shortcuts.
- **`:verb` (not `/verb`) for non-CRUD custom methods**: `/components/{name}/commands:issue`,
  `/auth/me:changePassword`, `/auth/me/sessions/{id}:revoke`, `/nodes:claim`,
  `/principals/{id}:disable`.
- **Singular kind sub-segments**: `/rules/calc`, `/property-types`,
  `/location-types`, `/types/event`.
- **official / private namespace** on every registry and rule family (below).
- **List conventions** (AIP-132 target): `filter` / `orderBy` / `pageSize`+
  `pageToken` (cursor, never offset) / `fields`. The `filter` runs through the one
  [expression engine](/architecture/expressions/) (Expr, one dialect, not pluggable), the same
  language across rule scopes, dynamic groups, and list filters.

The API is **self-describing**: the running server serves `GET /api/v1/openapi.json`,
`/openapi.yaml`, and a human reference page.

## The read side is views (backend-for-frontend)

Writes go through resource CRUD (each emitting an `audit_log` row in the same transaction).

:::design[Target design: the ViewResult contract, tracked in #523]

**Reads beyond a single resource go through views**, and views are part of the public API: a
**view** is a named query returning a uniform `ViewResult` (`{columns, rows}`), so one renderer
contract serves every view; **default views** ship with the binary, **private views** are
operator-saved *structured* queries, never raw SQL; `GET /views/{id}:run?param=` binds declared
params (undeclared or missing-required is a clean 400); and every view executes through the
**scoped Storage Gateway**, the safety boundary that lets the read side be a public BFF without
handing operators raw SQL. The full contract is [views](/architecture/views/).

:::

## The per-route gate

Every typed route carries a per-route coverage test (an `openapi_coverage_test.go`-style
gate) and the CLI-covers-every-route test, so the generated clients never fall behind the
API. After any route change: `make gen`, add the per-route test, keep the coverage tests
green.
