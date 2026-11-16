---
title: "A view binding reads its history and follows the tail from the same cursor"
date: 2026-11-16
status: Accepted
summary: "A view binding (a primitive, an entity selector, keys, a range, an optional step) is the one scoped read of the sample store a dashboard widget, an agent or the CLI makes. POST /views:run answers its history and the tail cursor the snapshot was taken at; GET /views:watch follows the in-process ingest tail from that cursor, every binding of a page over one SSE stream. view:read is the door, the binding's <kind>:read and its scope the fence. Diverges from the ViewResult design: a binding answers typed series and rows, not {columns, rows}."
refs: [1830, 523, 1829, 1161, 2050, 2051]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [views](/architecture/views/), [UI](/architecture/ui/), [API](/architecture/api/)
- **Decision:** (#1830, the read the dashboard widgets of #1831 to #1838 and the agent query tool of #2050 bind to.)
  1. **A binding is the read.** It names a primitive (`metric`, `property`, `event`, `log`), an entity
     selector (literal `refs`, or a `kind` and a `filter`), the `keys`, and a range (`from` and `to`, or
     `window_seconds` back from now), with an optional `step_seconds`. A metric or property binding answers
     one series per entity, key and instance, raw or on an epoch-aligned bucket grid; an event or log
     binding answers a page of rows, newest first, and a `next_page`. A selector past 200 entities, a raw
     read past 20,000 points or a grid past 5,000 buckets is a 422, never a truncated answer.
  2. **The cursor is the seam.** Every sample write commits to an in-process tail (a ring of 4,096 commits,
     its cursor `<boot>.<seq>`) inside the same write. `/views:run` reads every binding from one snapshot and
     answers the cursor it was taken at. A range with no `to` reads its raw rows with no upper bound, as the
     tail has none, so a row stamped ahead of the server's clock is in the history or after the cursor,
     never both and never neither.
     `/views:watch` takes each binding's cursor and carries only the rows after it.
  3. **One stream per page.** `/views:watch` multiplexes up to 32 bindings on one SSE connection. A `rows`
     event carries each binding's matching rows, its id the cursor after them; a `mark` advances an idle
     stream every 15 s. A reconnect's `Last-Event-ID` resumes each binding from the later of it and the
     binding's own cursor. A cursor the ring no longer holds (too old, or from before a restart) is a
     `reset` naming the binding, and the stream ends: the client re-runs that history and reconnects.
  4. **The door and the fence.** Both routes are gated by `view:read`; each binding also needs its kind's
     read (`component:read`, `system:read`) and resolves inside that read's scope. A filter selector
     resolves to the visible entities; a literal ref out of scope is the same 404 as one that does not
     exist. The watch resolves its selectors once, at connect, and its matcher carries only rows of the
     entities they resolved to.
  5. **Retention picks the source.** A metric step that is a whole number of hours and reaches past the raw
     retention answers the older hours from the hourly trends, and `covered_from` says where the data
     can begin. Every metric bucket carries `count`, `min`, `max`, `avg` and `last`, so a renderer picks
     its aggregate without a second read.
  6. **The console binds through one hub.** `createViewBinding` (in `web/src/lib/binding.ts`) reads a
     binding's history through the query cache, then subscribes it to the page's one `ViewHub`, which
     opens `/views:watch` with every subscribed binding's cursor (a second stream only past 32) and folds
     each live row into the history's state. The stream is a `fetch` with a body reader, not an
     `EventSource`, so a pasted bearer token rides it as it rides every call. The hub keeps each
     binding's cursor itself and reconnects from it. Platform health's sparklines are the first consumer:
     one binding per panel, its sampled keys on the server's platform component.
- **Context:** The [views](/architecture/views/) design names a `ViewResult` (`{columns, rows}`) behind
  `/views/{id}:run` for named, saved queries (#523), behind which every read, coded pages included, was
  to go. A chart of a series, an event feed and an agent's question about recent telemetry need the sample
  store read by entity, key and time, live, under the same scope as every other read; that is a read shape
  of its own, not a named query. The binding is it, and a named view (#2051) binds one. Coded console
  pages keep their typed reads: a view exists for the consumer no typed route can be written for in
  advance, so "everything read goes through a view" is withdrawn.
- **Consequences:**
  - In the API end-to-end tests a viewer scoped to one location reads only its entities through a kind
    selector, gets a 404 for a ref outside it, and sees none of its samples on the stream
    (`TestViewsBindingIsScoped`); 30 concurrent pushes across the run's snapshot each arrive exactly once
    (`TestViewsHistoryAndLiveMeetWithNoGapOrDuplicate`); a reconnect with `Last-Event-ID` repeats nothing
    (`TestViewsWatchResumesFromItsLastEventWithNoDuplicates`).
  - The tail is one server's ingest landing, in memory. Rows a calc or a rule derives are not on it, and a
    second server would not see the first's commits: a multi-server tail is a bus subject, not built.
  - A stream outlives a scope change made after it connected until it reconnects; its selectors resolve
    at connect.
  - The client's live fold takes the newest row to land as a bucket's `last`, which is the newest by time
    for a series that reports in order.
- **Not chosen:**
  - **Polling the history:** what the sparklines did, a request per series every 30 s, and a minute-old
    line on a page about whether the platform is keeping up.
  - **An `EventSource`:** it cannot send an `Authorization` header, so a bearer-token session could not
    stream.
  - **A stream per binding:** a dashboard of 30 widgets would hold 30 connections, past a browser's
    per-origin limit on HTTP/1.1.
  - **The bus as the tail:** it adds a hop and a subscription per stream for rows the writing server
    already holds; it is the path to many servers, not to one.
