---
title: "The dev fleet's collection-method table is generated from the fixtures (#1419)"
date: 2026-11-14
refs: [1419, 1370, 1266]
---

The simulation page and the lab guide named, in prose, which dev-fleet component exercises which
collection method and which methods are gaps, so a moved endpoint or a closed gap left them stale
with nothing failing. `devseed.MethodFactsJSON` now derives the table from the fixture's
`sim_endpoints` (resolving each driver's transport from the shipped specs and each component's place)
and from `devseed.MethodGaps`, which gained a `Why` beside its issue. `make gen` writes it through
`cmd/labgen`, a `CollectionMethods` component renders it on both pages (the lab guide scoped to the
boardroom), and a drift test fails when it goes stale. Unit tests hold the table to every
`sim_endpoints` entry and every gap. Closing a gap such as #1414 changes the page through `make gen`.
