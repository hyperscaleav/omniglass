---
title: "A series chart is truthful at every zoom (#1269)"
date: 2026-11-02
refs: [1269, 1265]
---

The series reads gain a `resolution`. Given it, `GET /{components,systems}/{name}/metrics/{metric}/samples`
and the property sibling return every regular bucket of the window instead of the newest raw rows:
epoch-aligned, empty buckets present with `count: 0`, 30 days at one hour exactly 720. A metric bucket
carries `min`, `max`, `avg`, `count` and `last`; a property bucket `count` and `last`. The body carries
`covered_from`, the edge of the type's effective retention, and nothing is read beyond it.

The chart draws a metric bucket as a min/max band under the avg line (a one-sample spike survives any
zoom) and a property as steps. A system's Data table now reads a day at five-minute buckets. The OpenAPI
document, the typed client, the CLI and the MCP catalog are regenerated.

Not in this slice: the gap break from a series' cadence (the avg line runs across an empty bucket meanwhile) and a failed check drawn as its outcome: that
is [#1475](https://github.com/hyperscaleav/omniglass/issues/1475), after task liveness (#1264).
