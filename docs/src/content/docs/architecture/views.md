---
title: Views
description: "The read side: a view is a named, parameterized, scope-checked query returning a uniform ViewResult, the backend-for-frontend every read goes through."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
Built today: the **view binding** ([below](#bindings-the-scoped-read-of-the-sample-store)), the one scoped
read of metric, property, event and log samples, with its history at `POST /views:run` and its live rows at
`GET /views:watch`, every binding of a page over one SSE stream (#1830,
[ADR-1830](/architecture/decisions/#adr-1830-a-view-binding-reads-its-history-and-follows-the-tail-from-the-same-cursor)).
Platform health's sparklines read through it. Not built: the named, saved view and its `ViewResult`
contract, in the design block that follows the binding.
:::

## Bindings: the scoped read of the sample store

A **binding** is what a chart, a feed, an agent or a script reads the samples through. It names:

- a **primitive**: `metric` or `property` (a series per entity, key and instance), `event` or `log`
  (rows, newest first, a page at a time);
- an **entity selector**: literal `refs` (names, uuids or dotted addresses), or a `kind` and a `filter`
  (a system's members, a product's components, a system type's systems);
- the **keys** (metric, property or event type names) and a **range**, `from` and `to` or a
  `window_seconds` back from now;
- an optional **step**, which answers a metric or property as an epoch-aligned bucket grid instead of
  raw points. A metric bucket carries `count`, `min`, `max`, `avg` and `last`, so the renderer picks the
  aggregate. A step that is a whole number of hours and reaches past raw retention answers the older
  hours from the [hourly trends](/architecture/storage/), and `covered_from` says where the data can begin.

A read too large to answer whole (past 200 entities, 20,000 raw points or 5,000 buckets) is a 422 naming
the limit, never a silently truncated answer: widen the step or narrow the selector.

### History and live meet at a cursor

Every sample the server lands is also committed, in the same write, to an in-memory **tail**, an ordered
log of recent commits with a cursor. `/views:run` reads every binding from one snapshot and answers the
cursor that snapshot was taken at; `/views:watch` takes each binding with its cursor and streams only the
rows after it. So a sample is either in the history or on the stream, never both and never neither, which
is the property a live chart needs to stay correct without a reload. Up to 32 bindings share one stream:

- a `rows` event carries each binding's new rows, its id the cursor after them;
- a `mark` event advances an idle stream's position every 15 seconds;
- a reconnect that sends the last id back (`Last-Event-ID`) resumes every binding with nothing repeated;
- a `reset` names a binding whose cursor the tail no longer holds (too old, or from before a restart),
  and its reader re-runs the history.

The tail is the server's own ingest landing, so a value a [calc](/architecture/calculations/) or a rule derives
is not on it, and a second server would not see the first's commits.

### Scope

Both routes need `view:read`, and each binding also needs its kind's read (`component:read`,
`system:read`) and resolves inside that read's [scope](/architecture/identity-access/). A kind selector
resolves to the entities the caller can see; a literal ref outside the scope is the same 404 as one that
does not exist, so a binding never confirms what it may not show. The stream resolves its selectors once,
when it connects, and carries rows of those entities only.

### In the console

`createViewBinding` (`web/src/lib/binding.ts`) is the console's primitive over it: it reads a binding's
history through the query cache, then subscribes it to the page's one hub, which holds a single
`/views:watch` for every binding on the page and folds each live row into the state the history began.
On [Platform health](/guides/operator/platform-health/) each server panel is one binding over its
sampled keys, and its sparklines move as the server records rather than on a poll.

::::design[Target design: the ViewResult contract, tracked in #523]

:::caution[Design: the ViewResult contract is entirely unbuilt]
The named-view framework below is not built: there is no `view` table, no `ViewResult` shape, no
`/views/{id}:run` route, and no renderer library. The binding above is the built read of samples a
named view can later bind. Today's read side is typed CRUD `GET`s plus six
hand-written composed reads (reachability, events, the component and node log
reads, the fleet projection at `GET /views/fleet`, the first route actually served under
`/views/`, and the platform projection at `GET /views/platform`, the second, behind
[Platform health](/guides/operator/platform-health/), with its closed-loop read at
`GET /views/platform/simulation`) that stand in until this framework lands,
which the [API](/architecture/api/) page's views-exception note covers.
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
  [namespace shadow](/architecture/properties/#key-scope-org-and-official) like the registries.
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
earns it, as a [binding](#history-and-live-meet-at-a-cursor) already does ([UI](/architecture/ui/), [time](/architecture/time/)).

## Versioning

A default view evolves **additively** within the API version (new columns, new optional params,
never a removal or a meaning change); a breaking change to a shipped view is a new view. A private
view is operator-owned data.

:::caution[Open question]
The structured view-definition grammar for private views (filter + order + fields + params), shared with
the [API](/architecture/api/) list filter language ([expressions](/architecture/expressions/)).
:::
::::
