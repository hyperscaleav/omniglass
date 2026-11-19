---
title: "A step chart starts on the value in force (#1488)"
date: 2026-11-15
refs: [1488, 1269]
---

A property drawn as steps started blank when it had not changed inside the window, so a value that
turned `on` two hours ago showed nothing on a one-hour chart. The bucketed property read now carries
`carried_in` (the newest measured row of the same instance before the grid, within retention) and a
per-bucket `held` that runs it through the leading empty buckets; the raw read gains `carried_in` too.
`chartLayout`'s step mode and `TimeseriesChart` take `carriedIn` and draw it from the left edge to the
first change, without making it a crosshair point. Integration tests cover the carry, the retention
floor, a different instance and a declared row, and the unchanged metric read; ADR-1269 records it.
