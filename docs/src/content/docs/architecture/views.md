---
title: Views
description: "The read side: a view is a named, parameterized, scope-checked query returning a uniform ViewResult, the backend-for-frontend every read goes through."
sidebar:
  badge:
    text: Partial
    variant: note
---

Writes go through typed resource CRUD; anything richer than one resource's `GET` reads through a
**view**: a named query returning the uniform **`ViewResult`**, executed through the scoped
[Storage Gateway](/architecture/storage/), a safe backend-for-frontend hit without touching raw
tables or writing SQL. The foundation is built: the contract, the in-code default registry, the
directory and run routes, and the first default view. Today's typed CRUD `GET`s and the
hand-written composed reads (reachability, events, reconciliation, the log reads) stand beside the
view routes until they migrate, which the [API](/architecture/api/) page's views-exception note
covers.

## Why a view layer

- A single resource reads through its typed `GET`; anything richer (the fleet-health grid, the
  cascade "why did this value win" explainer) is a **view**, a named query the platform ships, not
  a bespoke endpoint per page.
- **One shape, one renderer**: adding a view never adds a bespoke renderer or a raw query path
  ([UI](/architecture/ui/)).
- **One safety boundary**: no view ever runs unscoped or as raw SQL, which is what lets the read
  side be a public BFF.

## What a view is

A **default view** ships with the binary as declared Go (curated and PR-governed like seed data;
no `view` table): an addressable **name** (name-addressed like every registry, ADR-0062), a typed
**params schema**, a declared **permission**, its **columns and field-mapping**, and the scoped
query. The directory at `GET /views` publishes each view's whole client contract, so a caller
never guesses a param or a column.

:::design[Private views, tracked in #523]
Operator-saved **structured** queries (filter + order + fields + params), **never raw SQL**,
following the official / private namespace shadow like the registries. A private view resolves
against its author's visible set at run time, so a saved query is never a privilege escape. The
structured grammar is shared with the API list filter language ([expressions](/architecture/expressions/)).
:::

## ViewResult: the uniform shape

`ViewResult` is `{columns, rows, next_page_token}`: columns carry a name, a type, and a role hint,
and the view's **field-mapping** tells a renderer which column is the value, label, time, or
series key ([UI](/architecture/ui/)). Rows are positional cells against the column order.
**Cursor-paginated** where a view feeds, the [API](/architecture/api/) list convention
(`page_token` in, `next_page_token` out). Every view is a live scoped query; a hot view becomes a
materialized projection only when a read profile proves the live query too slow
([storage](/architecture/storage/)).

## Running a view

`GET /views/{name}:run` binds the view's typed params from repeated `param=name=value` query
pairs; an undeclared, duplicated, malformed, or missing-required binding is a clean 400 naming the
parameter, and a valid run is deterministic. The routes flow into the OpenAPI document, so the
typed SPA client and the cobra CLI are generated, never written:
`omniglass view run <name> --param name=value` drives the same route the console reads.

## Scope and safety

- All view routes are gated **`view:read`**; a run additionally requires the view's **declared
  permission** (`component:read` for the reachability grid), checked in the handler. The
  directory is readable with `view:read` alone (it publishes contracts, not data); running a
  view a caller is not entitled to is a 403 naming the permission.
- Every query runs in the gateway's **scoped mode**: the caller's `visible_set`
  ([identity and access](/architecture/identity-access/)) bounds every row with no per-view code.
- A view is **read-only** by construction (no writes, no side effects), which is what makes
  exposing views broadly safe.

## The default set

- **`component-reachability`**: every in-scope component interface with its latest
  `interface.reachable` verdict and its observation time; a never-probed interface reports the
  explicit `unknown` state, so the grid renders the whole fleet, not only the probed part.

:::design[The rest of the v1 default set, tracked in #523]
`event-feed` (cursor-paged, owner-filterable), `sample-history` (one owner and key over a window),
and `estate-counts` (the Home stat tiles), each a slice-tested named query.
:::

## How views are consumed

The API serves every view at `GET /views/{name}:run`, and the generated CLI mirrors it; both are
clients of the same OpenAPI contract.

:::design[The console binding and the MCP surface, tracked in #523]
Coded pages and dashboard widgets bind `view ref + renderer + field-mapping + params` through the
renderer library ([UI](/architecture/ui/)). An AI agent reaches the same views as tools on the
[MCP surface](/architecture/api/), scoped and audited like any caller. Config-dependent
presentation (a severity level's label and color) resolves client-side from the config view, not
baked into the result.
:::

## Live updates

`GET /views/{name}:watch` is an [SSE](/architecture/messaging/) stream with **notify-then-refetch**
semantics: the server pushes a change event (one on connect, the baseline, then one per delta), the
client re-runs the view, and `ViewResult` stays the only data shape. A quiet stream carries
heartbeat comments; a dropped connection reconnects on the stream's retry hint, and the fresh
baseline covers whatever was missed. The change detector re-runs the view under the **caller's**
scope, so a watcher is never notified of out-of-scope changes. V1 detects change server-side by
interval re-run plus result hash, an event only on delta. The generated CLI deliberately carries no
`watch` command (a one-shot command cannot print an endless stream); the console and any SSE client
consume it directly.

Admission is **per connection**, not per session: a stream runs the same gates `:run` runs
(`view:read`, then the view's declared permission), resolves the caller's visible set once, and
then ends at a **lifetime cap** so the client reconnects through those gates again. That bound is
what keeps a revoked grant from outliving a connection, and it is why a watched view's query must
impose a **total order** (a partial order lets Postgres reorder equal-keyed rows between runs, and
the detector reads order as content, so the hash would flap and every watcher would refetch every
interval forever).

The cost model is honest and bounded only by that cap: v1 runs **one full view query per open
connection per interval**, with no cap on watchers and no sharing between two clients watching the
same view under the same scope ([#546](https://github.com/hyperscaleav/omniglass/issues/546)). That
is sized for an operator console, not for fan-out, and the detector is what #430's bus feeds
replace. A watch on a paged view detects change on the **first page** only
([#547](https://github.com/hyperscaleav/omniglass/issues/547)).

:::design[The bus-fed relay, tracked in #523]
When the trusted-stream and CDC feeds land (#430), a bus consumer replaces the interval detector
under the same client contract; clients never change.
:::

## Versioning

A default view evolves **additively** within the API version (new columns, new optional params,
never a removal or a meaning change); a breaking change to a shipped view is a new view.
