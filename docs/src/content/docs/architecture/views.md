---
title: Views
description: "The read side: a view is a named, parameterized, scope-checked query returning a uniform ViewResult, the backend-for-frontend every read goes through."
sidebar:
  badge:
    text: Design
    variant: caution
---

::::design[Target design: the ViewResult contract, tracked in #523]

:::caution[Design: the ViewResult contract is entirely unbuilt]
Nothing on this page is built: there is no `view` table, no `ViewResult` shape, no `/views/{id}:run`
route, and no renderer library. Today's read side is typed CRUD `GET`s plus five hand-written composed
reads (reachability, events, reconciliation, and the component and node log reads) that stand in until
this framework lands, which the [API](/architecture/api/) page's views-exception note covers.
:::

Writes go through typed resource CRUD; in the target model **everything read goes through a view**:
a named query returning a uniform **`ViewResult`** (`{columns, rows}`), executed through the scoped
[Storage Gateway](/architecture/storage/), a safe backend-for-frontend hit without touching raw
tables or writing SQL. The [API](/architecture/api/) exposes it, the [UI](/architecture/ui/)
renders it, [API first](/contributing/api-first/) is the doctrine behind both.

## Why a view layer

- A single resource reads through its typed `GET`; anything richer (the fleet-health grid, the
  cascade "why did this value win" explainer) is a **view**, a named query the platform ships or an
  operator saves, not a bespoke endpoint per page.
- **One shape, one renderer**: adding a view never adds a bespoke renderer or a raw query path
  ([UI](/architecture/ui/)).
- **One safety boundary**: no view ever runs unscoped or as raw SQL, which is what lets the read
  side be a public BFF.

## What a view is

A `view` carries an id, a typed **params schema**, the query it runs, a **default / private** flag,
and the `official` boolean:

- **Default views** ship with the binary (curated, PR-governed, optionally Postgres-view-backed):
  the read surface the console's coded pages would query, an Alarms page reading a `firing-now`
  view rather than a bespoke route, the same view backing a dashboard widget unchanged.
- **Private views** are operator-saved **structured** queries (filter + order + fields + params),
  **never raw SQL**, following the official / private
  [namespace shadow](/architecture/properties/#key-scope-template-org-official) like the registries.
- **Parameterized**: typed params bound at run time at `/views/{id}:run?param=`; an undeclared or
  missing-required param is a clean 400.

## ViewResult: the uniform shape

`ViewResult` is `{columns, rows}`: columns carry a name, a type, and role hints, and a
**field-mapping** tells a renderer which column is the value, label, time, or series key
([UI](/architecture/ui/)). **Cursor-paginated** like the designed [API](/architecture/api/) list
convention (`page_token`). **Views by default, materialized only when earned**: a hot view becomes
a materialized projection only when a read profile proves the live query too slow
([storage](/architecture/storage/)).

## Scope and safety

- Every view runs in the gateway's **scoped mode**: the caller's `visible_set`
  ([identity and access](/architecture/identity-access/)) filters every view with no per-view code.
  A private view **cannot widen its author's scope** (it resolves against their visible set at run
  time), so a saved query is never a privilege escape.
- A view is **read-only** by construction (no writes, no side effects), which is what makes
  exposing views broadly (the API, an MCP tool, a shared dashboard) safe.
- Config-dependent presentation (a severity level's label and color) resolves client-side from the
  config view, not baked into the result.

## How views are consumed

One read contract, three consumers:

- **The console**: coded pages and dashboard widgets both bind
  `view ref + renderer + field-mapping + params` through the renderer library
  ([UI](/architecture/ui/)), which arrives with this contract.
- **The API**: every view at `/views/{id}:run`, part of the public contract.
- **An AI agent**: view-backed tools on the [MCP surface](/architecture/api/) (the agent's search
  and query tools *are* views), scoped and audited like any caller.

## Live updates

A view read is **query-polling by default** (slow-changing config uses a long stale time); a view
may **stream** over a server-side [SSE](/architecture/messaging/) relay where latency or fan-out
earns it ([UI](/architecture/ui/), [time](/architecture/time/)).

## Versioning

A default view evolves **additively** within the API version (new columns, new optional params,
never a removal or a meaning change); a breaking change to a shipped view is a new view. A private
view is operator-owned data.

:::caution[Open question]
The structured view-definition grammar for private views (filter + order + fields + params), shared with
the [API](/architecture/api/) list filter language ([expressions](/architecture/expressions/)).
:::
::::
