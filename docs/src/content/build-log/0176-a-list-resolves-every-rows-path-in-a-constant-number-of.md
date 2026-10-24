---
title: "A list resolves every row's path in a constant number of queries"
date: 2026-08-09
refs: []
form: bullet
---

- **A list resolves every row's path in a constant number of queries**
  ([#643](https://github.com/hyperscaleav/omniglass/issues/643)). The path attach the entry above
  shipped ran `PathOf` once per row, two or three queries each, so an unpaginated `GET /components`,
  `/systems` or `/locations` cost tens of thousands of sequential round trips at estate scale and
  re-paid them on every write through the console's query invalidation. A new `PathsOf` walks a whole
  page at once: the same recursive CTEs, the same `CYCLE` guard, seeded with every id and carrying the
  seed through the recursion as `origin`, so a page costs three queries for the two accessor planes
  (the rows' own chains, their plane roots, and the distinct plane-root locations) and one for
  locations, whatever the page size. `PathOf` stays, as the single-row entry point and as the oracle
  the new equivalence test holds the batch walker to row for row. The `scopedConfig` hook became a
  batch one (`attachPaths`), taking a slice rather than a row, so `GET` and `LIST` render through the
  identical code instead of drifting apart down two paths; the bare render's abbrev lookup, still
  `LIST`-skipped, now resolves once per distinct product rather than once per row. No wire change:
  `path`, `path_segments` and both renders are byte-identical to what the per-row walk produced.
