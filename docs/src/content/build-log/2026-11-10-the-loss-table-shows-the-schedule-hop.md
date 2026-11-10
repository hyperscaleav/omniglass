---
title: "The loss table shows the schedule hop, runs due versus runs made (#1693)"
date: 2026-11-10
refs: [1693, 1641, 1634, 1659, 1692, 1739]
---

A node's dispatcher now counts the schedule hop: every due on a task's schedule in, every run it
makes out, per kind the run produces (a reachability probe's metric and property, a driver poll's the
lanes its emits declare), so a due passed over, behind a run still in flight on its lane or a late
pass, reads as that hop's loss with the cause `skipped`. A probe an upstream holds counts made, and a
run off the schedule is no due. The node writes the three delivery keys at the instance
`schedule/{kind}` on its own component every report window, beside its spool's. `omniglass lab
measure` reads them over the window and prints a `schedule` row ahead of each kind's `ingest` row, in
the same form as every hop, with no row for a kind no scheduled task produces; end to end composes it.
The integration test points a 1 s poll at a device that never answers and reads the schedule loss
above 0 while the ingest hop reads 0: a skipped due made no item to lose. Platform health draws the
hop next (#1739).
