---
title: "Preview an event rule against recent samples (#959)"
date: 2026-10-21
refs: [959]
---

The fourth slice of the expression engine (#524). `POST /event-rules:preview` (gated `rule:read`,
`omniglass event-rule preview`) takes a candidate rule (sample key, selector, fire and optional clear
criteria; sustains accepted and ignored) and answers what it would have done, writing nothing: the
gateway's `PreviewEventRule` runs in a read-only transaction and compiles the criteria once. As first
built it read the key's lane twice, the newest sample before the window of every series and every
sample in it, capped at `RulePreviewRowCap` (5,000) rows each. A review proved that read wrong at scale
and in four ways, and it was rebuilt before the rollup merged (below). Each selected series is
walked as the ingest would walk it with no sustains, from the state its pre-window sample sets: a fire
out of force is a fire edge, a clear in force a clear edge. The answer carries the edges, the series in
force now (with the window's fire that brought each in, absent when it was in force before the
window), the window, the cap, `truncated` and `sustains_ignored: true`. A bad criterion is a 422 at
its line and column. Tests seed a reachability window and check the edges and the in-force set, a
component-scoped and a place-scoped viewer, the truncation flag at a lowered cap, and that the event,
alarm, condition, streak, audit and rule row counts do not move.
