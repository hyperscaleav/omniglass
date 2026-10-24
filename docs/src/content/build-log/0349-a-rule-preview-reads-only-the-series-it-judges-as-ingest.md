---
title: "A rule preview reads only the series it judges, as ingest would (#959)"
date: 2026-10-21
refs: [959]
---

A review of the preview, with EXPLAIN ANALYZE on 200,000 rows, found its seed read scanned the key's
whole history and its window read sorted every window row before the cap; the selector ran in Go after
the capped read, so unselected rows spent the cap; a series seeded from its newest sample before the
window missed a hysteresis rule's state (fire above 80, clear below 70: 85, then 75, then 60); the cap
cut series in half; a late sample was judged though the ingest skips it; and a selector name resolved
fleet-wide said whether a component outside the caller's scope existed. Now the selection is SQL
(the named components and the type subtrees, within the read scope), each selected component's
instances are found by a loose index skip on the series index, and each series is two index probes: its
starting state from the newest deciding sample within `RulePreviewSeedLookback` (7 days, probing at most
`RulePreviewSeedProbe` samples; none, and the series is listed in `state_unknown`), and its window. A
window sample older than one that arrived before it decides nothing, the ingest's stale check. The cap
takes whole series, reported as `series_judged` of `series_total`; selector names resolve in the
caller's scope, so a hidden component is the same 422 as a missing one. The console's preview panel
counts the series it judged and those that started unknown. Tests cover each case, and an EXPLAIN test
asserts every per-series read on both lanes is an index probe on its series index with no seq scan.
