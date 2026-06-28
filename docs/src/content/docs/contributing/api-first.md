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
  `/types/component`, `/types/event`.
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
