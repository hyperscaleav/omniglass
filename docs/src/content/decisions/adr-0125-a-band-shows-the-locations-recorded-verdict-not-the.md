---
title: "A band shows the location's recorded verdict, not the console's fold"
date: 2026-08-15
status: "Accepted"
summary: "A fleet band's verdict chip renders the location's server-recorded verdict, the same row its detail page reads, never the console's fold over in-scope clusters; the fold remains as a named derivation. A band disagreeing with its own detail one click apart is a visible contradiction, and the fold covers only what the caller may read"
---

- **Date:** 2026-08-15 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/), [health](/architecture/health/)
- **Decision:** a fleet band's verdict chip renders `FleetLocationBody.verdict`, the location's
  own server-recorded row, resolved through the grouping's optional `recordedVerdict` seam. The
  console's fold over in-scope clusters (`Band.verdict`, worst-wins) stays available, named for
  what it is, and no chip renders it.
- **Context:** the band navigates to `/locations/{id}`, whose badge reads the same recorded row;
  a band that says healthy beside a detail page that says outage, one click apart, is a visible
  contradiction. The fold also covers only the clusters the caller may read, so under a narrow
  scope it is not the location's verdict at all. The console-computes-no-verdict rule
  (`web/src/lib/health.ts`) already pointed here; this records the band as a consumer of it.
- **Tracked under** epic [#630](https://github.com/hyperscaleav/omniglass/issues/630), slice
  [#633](https://github.com/hyperscaleav/omniglass/issues/633).
