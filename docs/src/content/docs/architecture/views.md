---
title: Views
description: "How the read side for consumers no route was written for is built: a view binding reads samples inside the caller's scope, its history then its live tail from one cursor."
sidebar:
  order: 52
  badge:
    text: Partial
    variant: note
---

A page built into the console reads through typed routes written for it. A dashboard widget pointed at
any system, an agent's question, or a script's chart has no route written for it in advance, so it
reads through a **view binding**: the one scoped read of the sample store, with its history at
`POST /views:run` and its live rows at `GET /views:watch`
([ADR-1830](/architecture/decisions/#adr-1830-a-view-binding-reads-its-history-and-follows-the-tail-from-the-same-cursor)).
Both are in the [API reference](/reference/api/); this page is how they behave.

## Bindings: the scoped read of the sample store

A binding names four things:

- **a primitive**: `metric` or `property`, answered as a series per entity, key and instance; or
  `event` or `log`, answered as rows, newest first, a page at a time;
- **an entity selector**: literal `refs` (names, uuids or dotted addresses), or a `kind` (component or
  system) and a filter (a system's members, a product's components, a system type's systems);
- **the keys**: the metric, property or event type names to read;
- **a range**: `from` and `to`, or `window_seconds` back from now.

An optional `step_seconds` answers a metric or property as an epoch-aligned grid of buckets instead
of raw points. A metric bucket carries `count`, `min`, `max`, `avg` and `last`, so the renderer picks
the aggregate. A step that is a whole number of hours and reaches past raw retention reads the older
hours from the hourly trends ([storage](/architecture/storage/)).

A read too large to answer whole (past 200 entities, 20,000 raw points or 5,000 buckets) is a 422
naming the limit, never a silently truncated answer.

## History and live meet at a cursor

Every sample the server lands is also committed, in the same write, to an in-memory **tail**: an
ordered log of recent commits with a cursor (`internal/tail`). `/views:run` reads every binding from one
snapshot and answers the cursor it was taken at; `/views:watch` takes each binding with its cursor and
streams only what came after. A sample is in the history or on the stream, never both and never
neither, which is what a live chart needs to stay right without a reload.

One stream carries up to 32 bindings, as server-sent events:

- a `rows` event carries each binding's new rows, its id the cursor after them;
- a `mark` event moves an idle stream's position every 15 seconds;
- a reconnect that sends its last id back (`Last-Event-ID`) resumes every binding with nothing repeated;
- a `reset` names a binding whose cursor the tail no longer holds, and ends the stream, so its reader
  runs the history again.

The same stream carries **changed notices** for the `topics` a watch names, a component or a system
([ADR-2731](/architecture/decisions/#adr-2731-the-console-follows-state-over-its-one-live-stream-and-polling-is-guarded-out)).
A write that moves one (a health recompute, which every alarm, slot and member change makes) lands a
notice on the tail's change lane once its transaction commits, and a `changed` event names the topic,
at most once each 250 ms per stream. It carries no data; the reader refetches the route it reads.
The topics resume from the stream's last event id (`topics_cursor`, or `Last-Event-ID`), and one the
tail no longer holds is a `reset` with `topics: true`.

The tail holds the newest 4,096 commits or 64 MiB of rows, whichever is less, so a burst cannot grow it
without bound. It is the server's own ingest landing: a value a rule or a calc derives is not on it,
and a second server does not see the first's commits.

## Scope

Both routes need `view:read`, and each binding (and each topic) also needs its kind's read (`component:read` or
`system:read`) and resolves inside that read's [scope](/architecture/identity-access/). A filter
selector resolves to the entities the caller can see; a literal ref outside the scope is the same 404
as one that does not exist.

A stream resolves its selectors once, when it opens, so it is bounded to keep that answer fresh: it
ends after 15 minutes, and on each 15 second heartbeat it rechecks its caller, ending once the session
no longer resolves or the caller's grants changed. The client reconnects from its cursor at once, and
the reconnect authenticates and resolves again. One user holds at most 16 watches open, 1,024 across
the server; past either, opening one is a 429.

## Who reads through it

- **The console**: `createViewBinding` (`web/src/lib/binding.ts`) reads a binding's history through
  the query cache and subscribes it to the tab's one hub (`web/src/lib/live.ts`), which opens as few
  watches as hold the page's bindings and its topics. Platform health's sparklines are bindings, and
  the system page's health, slots and members follow its topic through `liveQuery`
  ([UI](/architecture/ui/)).
- **The CLI**: `omniglass view run` and `omniglass view watch`, generated from the routes.
- **An agent**: `run-views` on the [MCP tools](/reference/mcp/) catalog, under the agent's token and
  scope like any other call.

:::design[An agent's fleet query tool over the binding, tracked in #2050]
An agent's fleet query tool, and the analytics skill over it, call the binding rather than a read path
of their own.
:::

::::design[Named, saved views, tracked in #2051]
A **view** is a named query over a binding that the platform ships or an operator saves, run by its
id at `/views/{id}:run` with typed parameters bound at run time (an undeclared or a missing required
parameter is a 400).

- **A shipped view** comes with the binary, curated and reviewed, read unchanged by a widget or an
  agent.
- **A saved view** is an operator's structured query (filter, order, fields, parameters), never raw
  SQL, in the operator's namespace beside the shipped ones as every catalog is.
- **It answers what its binding answers**, typed series or rows, so a widget that renders one view of
  a primitive renders every view of it.
- **It cannot widen its author's scope**: it resolves against whoever runs it, at run time, so a saved
  query is never a privilege escape, and it is read-only by construction.
- **It evolves additively**: a shipped view gains fields or optional parameters and never loses one;
  a breaking change is a new view.

:::caution[Open question]
The grammar a saved view's filter is written in, shared with the API's list `filter`
([expressions](/architecture/expressions/)).
:::
::::
