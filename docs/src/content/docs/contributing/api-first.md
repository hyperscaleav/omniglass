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
artifacts.** A drift check in CI fails the PR if the committed artifacts are stale.

## The generation pipeline

| Generator | Input | Output | Consumer |
|---|---|---|---|
| `cmd/openapigen` | Huma Go structs | `api/openapi.json` (+ `.yaml`) | everything below |
| `web pnpm gen:api` | `openapi.json` | `web/src/api/schema.gen.ts` | typed `openapi-fetch` SPA client |
| `cmd/cligen` | `openapi.json` | `internal/cli/api_gen.go` (cobra) | the CLI, patched via `api_hooks.go` |
| `cmd/mcpgen` | `openapi.json` | the MCP server (a curated tool catalog) | AI agents over the [API contract](/architecture/api/) |
| `cmd/schemagen` | authoring structs | `schema/*.schema.json` | YAML editor validation (VSCode) |
| `gen-proto` | `proto/og/v1/*.proto` | committed `*.pb.go` | the gRPC ingest path |

One command runs them all (`make gen`); each has a focused target (`make gen-api`,
`gen-cli`, `gen-schema`, `gen-proto`). The committed `*.pb.go` and JSONSchema let a
contributor build without protoc or a running server.

## A name is the address, a uuid is identity

**Every response carries both forms of a reference: the name an operator reads and the id it
resolves to.** `{"parent": "rack", "parent_id": "0198f..."}`. The name is what a human types
and what a body round-trips; the id is the stable handle that survives a rename. A response
that carries only the uuid is the failure this rule names.

The test is a **round trip**: a response body can be fed back to the write that produced it.
Create a component with `{"parent": "rack"}` and read it back as `{"parent": "rack"}`, not as
`{"parent_id": "0198f2c4-..."}`. When that fails, every client has to fetch a second
collection and join by uuid to render one label, and they each do it slightly differently.

Two exceptions, both narrow:

- **An entity with no name** is legitimately addressed by id: an interface (its name is unique
  only within its component), a stored property value, an audit row, a grant, a principal.
- **A slug-keyed catalog** already satisfies the rule, because its id *is* the name:
  `product_id: "cisco-room-bar"`.

**Every foreign key stores the target's primary key**, which for an estate entity is its uuid.
A rename then has nothing to rewrite: the friendly name is free to change precisely because
nothing points at it. A `_id` column holding a name, kept alive by `on update cascade`, is the
shape this rule exists to prevent; the cascade is machinery that only exists to fund the wrong
choice. The one class of exception is the **slug-keyed catalog** (`product`, `standard`,
`property`, `interface_type`), where the name *is* the primary key and pointing at it is
pointing at the key.

**A path or a join field accepts either form.** `GET /components/{ref}` and a body's
`{"parent": "..."}` both take a uuid or a name; the uuid is tried first, so an id never
collides with a name. Operators type names, scripts hold ids, and neither has to convert.

`TestResponsesAddressEntitiesByName` enforces this over the generated OpenAPI, so a body
cannot reintroduce a uuid reference silently. Its allow-list is the whole of the exception,
and adding to it is a decision: if the target has a name, carry the name.

## Conventions (AIP-style)

These are the conventions a route follows while you write it; the complete [API
contract](/architecture/api/) (the error envelope, idempotency, long-running operations,
versioning, and the authorization status mapping) is the architecture of record.

Every operation lives under `/api/v1/*`. The path shape is derivable, not special-cased:

- **Plural collections**, standard CRUD by primary key: `POST` creates (409 on PK
  collision), `GET` reads, `PATCH` updates by PK (AIP-134, partial), `DELETE` removes.
  No upsert/register shortcuts.
- **`:verb` (not `/verb`) for non-CRUD custom methods**: `/alarms/{id}:ack`,
  `/nodes/{name}:heartbeat`, `/rules/calc:validate`, `/components/{name}:apply`,
  `/views/{id}:run`.
- **Singular kind sub-segments**: `/rules/calc`, `/datapoints/metric`,
  `/location-types`, `/types/event`.
- **official / private namespace** on every registry and rule family (below).
- **List conventions** (AIP-132 target): `filter` / `orderBy` / `pageSize`+
  `pageToken` (cursor, never offset) / `fields`. The `filter` runs through the one pluggable
  expression engine ([Expr by default](/architecture/expressions/)), the same language
  across rule scopes, dynamic groups, and list filters.

The API is **self-describing**: the running server serves `GET /api/v1/openapi.json`,
`/openapi.yaml`, and a human reference page.

## The read side is views (backend-for-frontend)

Writes go through resource CRUD (each emitting an `audit_log` row in the same transaction).
**Reads beyond a single resource go through views**, and views are part of the public API:

- a **view** is a named query backing a page or widget, returning a uniform `ViewResult`
  (`{columns, rows}`) so one renderer contract serves every view;
- **default views** ship with the binary (curated, may be Postgres-view-backed, PR-
  governed); **private views** are operator-saved *structured* queries (filter + order +
  fields), never raw SQL;
- `GET /views/{id}:run?param=` binds declared params; undeclared or missing-required
  params are a clean 400;
- views execute through the **scoped Storage Gateway**, so IAM scope applies to a view's
  results exactly as to any read. This is the safety boundary that lets the read side be a
  public BFF without handing operators raw SQL.

## The per-route gate

Every typed route carries a per-route coverage test (an `openapi_coverage_test.go`-style
gate) and the CLI-covers-every-route test, so the generated clients never fall behind the
API. After any route change: `make gen-api && make gen-cli`, add the per-route test, keep
the coverage tests green.
