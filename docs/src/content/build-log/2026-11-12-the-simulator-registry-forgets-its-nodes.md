---
title: "The simulator registry forgets deleted and silent nodes (#907)"
date: 2026-11-12
refs: [907, 887]
---

The server's simulator registry never removed a node: one deleted after its simulator pulled stayed in
`GET /simulation` until the server restarted. Deleting a node now drops its entry, and a node whose
simulator has neither pulled nor reported for a hundred seconds (ten default pulls) is dropped on the
next read, so a simulator torn down without its node being deleted ages out instead of reading
`offline` for good. The simulation page records that the hits are per server process: with more than one
replica a simulator's status is visible only on the replica its node's bus session landed on, and a
shared store is the path if that ever matters.
