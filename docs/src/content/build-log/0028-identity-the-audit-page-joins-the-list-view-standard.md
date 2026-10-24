---
title: "Identity: the audit page joins the list-view standard"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: the audit page joins the list-view standard.** The Audit page swaps its bare table for the
  console's shared **FilterBar** faceted search (the same one the inventory lists use): filter by `who`,
  `action`, `resource`, or `id`, chips combining to narrow, matched client-side over the loaded rows via the
  `lib/predicate` engine. **Load older** pages backward through the server `before` cursor, so a filter that
  comes up short is a cue to load deeper. Proven by a predicate unit test (each facet, within-chip OR and
  cross-chip AND, the sorted value catalog) and a component test (the FilterBar narrows the rows, and load-older
  asks for events older than the oldest loaded). Deferred: a chip time-range facet (the shared `matchOp`
  compares `gt`/`lt` numerically, so an ISO-timestamp facet needs a date-aware operator in the predicate
  primitive first).
