---
name: add-api-route
description: "Use when adding or changing a Huma API operation (a route, a custom :verb method, or its request/response shape): the registration and gated(...) permission stamp, the Storage Gateway patterns (scoped-CRUD primitive, all-scope directory, in-transaction audit row), the ADR-0062 name-vs-uuid addressing, the doc-tag pipeline, and the make gen ripple. The middle of the vertical slice, between /storage-schema-change and the UI skills."
---

# Add or change an API route

The API is the source of truth (doctrine 1): the Go operation is written first and
everything else (OpenAPI, CLI, SPA client, CLI reference) is generated from it. Trust the
code paths named here over architecture prose; api.md carries known drift.

## Registration and the permission stamp

- A resource's routes live in `internal/api/<resource>.go` and register with
  `huma.Register(api, a.gated(huma.Operation{...}, "<resource>", "<action>"))`.
- Paths are AIP-style: `/components`, `/components/{name}`, and custom methods as
  `:verb` suffixes (real examples: `/tags/{name}:values`, `/tags/{name}:setPlatform`,
  `.../{name}:listTags` in `internal/api/tags.go`).
- `gated(...)` (`internal/api/auth.go:264`) is the only door: it joins the tokens into
  the permission, records it in the permission registry, stamps
  `x-omniglass-permission` on the operation, and prepends the `authn` and `require`
  middlewares. A write that lands at the platform tier adds `platformGated(...)` for its
  second permission.
- **Never register a gated route without `gated(...)`.** The spec-contract tests
  (`internal/api/authz_guard_test.go`) fail any operation with no stamp unless it is in
  the deliberate ungated allow-list (authn-only self routes and the public
  `POST /nodes:claim`); do not grow that list casually.
- The permission must resolve against the seeded role matrix (`internal/seed/roles.yaml`):
  a stamp no role holds gates nothing and teaches a phantom permission in the Roles view.

## The Storage Gateway (the only DB path)

Pick the read pattern deliberately:

- **Scoped list/get on a tree entity:** use the scoped-CRUD primitive, one
  `<entity>Config` plus `scopedList` / `scopedGet` / `scopedDelete`
  (`internal/storage/components.go:113` is the exemplar). Scope is injected by the
  primitive; do not hand-roll the query.
- **All-scope admin directory:** refuse early without an all read
  (`ListVariables` in `internal/storage/variables.go`: `if !read.All { return nil,
  ErrVariableForbidden }`), then query without per-row filtering.
- **Anti-exemplar, do not copy:** `ListSecrets` (`internal/storage/secrets.go:258`)
  selects every row unscoped and filters per row in Go, violating the scope-injection
  invariant; #431 tracks the fix.

Mutations run in one transaction: begin, validate, write, **`writeAuditRes(ctx, tx,
actorID, verb, resource, resourceID, old, new)` in that same transaction**
(`internal/storage/locations.go:554`; `CreateComponent` in
`internal/storage/components.go` is the exemplar), then commit. A committed privileged
change without its audit row is a red gate (ship-slice gate 10). Auth events on the
no-transaction path go through `WriteAuthEvent` (`internal/storage/audit.go:82`).

A **new Gateway method** ripples: declare it on the `Gateway` interface
(`internal/storage/storage.go:28`), implement it on `PG`, and add the
`UnimplementedGateway` stub (`internal/storage/unimplemented.go`), or dependent packages
fail to compile. Not-found wraps `pgx.ErrNoRows`.

## Addressing (ADR-0062)

The uuid is identity; the `name` is the operator-facing address. Routes take `{name}`
(`GetComponent(ctx, name, read)`), responses carry both, and no operator surface renders
or filters by uuid. A `_name`-suffixed field holding a uuid is a defect.

## Shapes and the doc-tag pipeline

Request/response structs carry `doc:` tags on every field. They flow to OpenAPI, then to
the generated CLI flag help and the typed SPA client, so **a blank `doc:` tag ships blank
operator-facing help** (a shipped regression class). Descriptions are linted docs: keep
them current with behavior and free of retired vocabulary.

## The gen ripple

After the Go changes, run `make gen` and commit everything it touches:
`api/openapi.json` + `api/openapi.yaml`, `internal/cli/api_gen.go`,
`docs/src/content/docs/reference/cli/index.md`,
`docs/src/content/docs/architecture/data-model.md`, `web/src/api/`, `proto/og/v1/`.
CI (`gen-drift.yml`) fails the PR on any stale artifact, using pinned protoc versions;
regenerate with the same pins (the SessionStart env check warns on a mismatch).

Docs follow the surfaces (ship-slice gate 4): a route change lands in
`architecture/api.md`; if `internal/cli/api_gen.go` moved, the CLI guide
(`guides/cli.md`) is in scope; a console surface change takes its guide and screenshots.

## RED -> GREEN

Write the behavior test first at the right tier: the authz matrix and guard tests cover
the stamp automatically, so your tests prove the *behavior* (the scoped read excludes the
out-of-scope row; the mutation writes its audit row; the custom method settles the
outcome), integration-tier against real Postgres via testcontainers, e2e driving the
route as the user would. Then the handler, then green, then `make gen`.
