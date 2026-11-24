---
title: "The lab's ingest lag refuses when the lane is behind (#1897)"
date: 2026-11-23
refs: [1897, 1896, 1623]
---

The lab's ingest lag p95 timed only the samples that had landed, so on the 3,000 soak it fell from
237.6 s to 0.25 s while the backlog grew. The lag is now refused as not measured when the closed
loop's `published-not-landed` share for the window is above 0, with the share as the reason. When the
closed-loop read is missing, a cadence estimate (rows landed against the probes the poll targets
should have drawn) stands in, refuses past 5 %, and is labelled a floor estimate. The closed loop's
denominator is read from the outposts' tallies, not from ingest. Regression tests:
`TestRefuseBehindLag*` in `internal/labfootprint`.
