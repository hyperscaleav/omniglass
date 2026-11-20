---
title: "A view binding reads history and streams live over one connection (#1830)"
date: 2026-11-16
refs: [1830, 523, 1831, 1161, 2050]
---

The sample store has one scoped read now. A **view binding** names a primitive (metric, property, event
or log), an entity selector (refs, or a kind and a filter), the keys and a range, with an optional bucket
step; `POST /views:run` answers each binding's history from one snapshot with the tail cursor it was taken
at, and `GET /views:watch` follows every binding of a page over one Server-Sent Events stream from those
cursors. Every sample write commits to an in-process tail inside the same write, which is what makes the
cursor a clean seam: 30 concurrent pushes straddling a run each arrive exactly once across the history and
the stream. Both routes are gated by `view:read` plus the binding's kind read, and resolve inside its scope:
a viewer scoped to one location reads only its entities, gets a 404 for a ref outside it, and sees none of
its rows on the stream. A dropped stream resumes from `Last-Event-ID` with nothing repeated, and a cursor
the tail no longer holds is a `reset` that sends the client back to the history. The generated CLI gained
`omniglass view run` and `omniglass view watch`, and `run-views` is on the MCP catalog (the agent's query
tool over it is #2050). In the console, `createViewBinding` reads a binding's history and folds its live rows in through one shared hub, and
Platform health's sparklines moved onto it: one binding per panel instead of a poll per row every 30
seconds, so a panel's line moves as the server records. A watch is bounded so its scope never goes stale:
it ends at a 15 minute lifetime, and on a heartbeat once its caller's session or grants changed, and the
client reconnects from its cursor; one user holds at most 16. The tail ring holds 4,096 commits or 64 MiB
of rows, whichever is less. The browser specs and the docs screenshots no longer wait for `networkidle`,
which a page holding a live stream never reaches: a shared `settle` waits for every request but a stream.
The decision is ADR-1830.
