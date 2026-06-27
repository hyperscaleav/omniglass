---
title: Views
description: "The read side: a view is a named, parameterized, scope-checked query returning a uniform ViewResult, the backend-for-frontend every read goes through."
sidebar:
  badge:
    text: Design
    variant: caution
---

Writes go through typed resource CRUD; **everything read goes through a view**. A view is a named query
that returns a uniform **`ViewResult`** (`{columns, rows}`) and executes through the scoped
[Storage Gateway](/architecture/storage/), so the read side is a safe backend-for-frontend that the
console, the [API](/architecture/api/), and an AI agent all hit without ever touching raw tables or
writing SQL. This page is the read contract; the [API](/architecture/api/) is the surface that exposes
it, the [UI](/architecture/ui/) is the renderer that consumes it, and [API first](/contributing/api-first/)
is the doctrine behind both.

## Why a view layer

- A single resource reads through its typed `GET` (the [API](/architecture/api/) standard methods).
  Anything richer, a cross-entity aggregate, a fleet-health grid, the cascade "why did this value win"
  explainer, is a **view**: a named query the platform ships or an operator saves, not a bespoke
  endpoint per page.
- **One shape, one renderer.** Every view returns `ViewResult` (`{columns, rows}`), so one renderer per
  shape serves every view ([UI](/architecture/ui/)); adding a view never adds a bespoke renderer or a
  raw query path.
- **One safety boundary.** A view runs through the **scoped gateway**, so a caller sees only the rows in
  its visible set, exactly as for any read. The read side can be a public BFF precisely because no view
  ever runs unscoped or as raw SQL.

## What a view is

A `view` carries an id, a typed **params schema**, the query it runs, a **default / private** flag, and
the `official` boolean:

- **Default views** ship with the binary (curated, PR-governed, optionally backed by a Postgres view).
  They are the read surface the console's coded pages query: the Alarms page reads the `firing-now`
  view, not `GET /alarms` directly, so the read contract stays uniform and the same view backs a
  dashboard widget unchanged.
- **Private views** are operator-saved **structured** queries (filter + order + fields + params),
  **never raw SQL**. They follow the official / private
  [namespace shadow](/architecture/datapoints/#key-scope-template-org-official) like the registries.
- A view is **parameterized**: it declares typed params bound at run time. The
  [API](/architecture/api/) runs one at `/views/{id}:run?param=`; an undeclared or missing-required
  param is a clean 400.

## ViewResult: the uniform shape

`ViewResult` is `{columns, rows}`: each column carries a name and type (plus role hints a renderer maps),
and rows are the records. The shape is uniform so the renderer library is decoupled from any specific
view; a **field-mapping** tells a renderer which column is the value, label, time, or series key
([UI](/architecture/ui/)).

- **Cursor-paginated** like any [API](/architecture/api/) list (`page_token`), over the already-scoped
  result.
- **Views by default, materialized only when earned**: most views are live queries; a hot view becomes
  a materialized projection only when a read profile proves the live query too slow (the same discipline
  as [storage](/architecture/storage/)).

## Scope and safety

- Every view runs in the gateway's **scoped mode**: the caller's `visible_set`
  ([identity and access](/architecture/identity-access/)) filters the rows, on every view, with no
  per-view code. A private view an operator saves **cannot widen their scope**: it resolves against
  their visible set at run time, so a saved query is never a privilege escape.
- A view is **read-only** by construction: it never writes and has no side effects, which is what makes
  exposing views broadly (to the API, an MCP tool, a shared dashboard) safe.
- Presentation that depends on config (a severity level's label and color) resolves client-side from the
  config view, not baked into the result.

## How views are consumed

One read contract, three consumers:

- **The console** renders a view through the renderer library ([UI](/architecture/ui/)): coded pages and
  dashboard widgets both bind `view ref + renderer + field-mapping + params`.
- **The API** exposes every view at `/views/{id}:run` ([API](/architecture/api/)); views are part of the
  public contract.
- **An AI agent** reads through view-backed tools on the [MCP surface](/architecture/api/) (the agent's
  search and query tools *are* views), scoped and audited like any caller. The read side is one contract
  whether a human, a script, or an agent asks.

## Live updates

A view read is **query-polling by default** (a refetch interval; slow-changing config uses a long stale
time). A view may **stream** over a server-side [SSE](/architecture/messaging/) relay where latency or
fan-out earns it, the same earn-it-with-a-profile discipline ([UI](/architecture/ui/),
[time](/architecture/time/)).

## Versioning

A default view evolves **additively** within the API version (new columns, new optional params, never a
removal or a meaning change); a breaking change to a shipped view is a new view. A private view is
operator-owned data.

:::caution[Open question]
The structured view-definition grammar for private views (filter + order + fields + params), shared with
the [API](/architecture/api/) list filter language ([expressions](/architecture/expressions/)).
:::

Related: [API](/architecture/api/) (the surface and `/views/{id}:run`), [UI](/architecture/ui/) (the
renderer and the field-mapping), [identity and access](/architecture/identity-access/) (the scope a view
runs in), [storage](/architecture/storage/) (materialize-when-earned), and
[API first](/contributing/api-first/) (the doctrine).
