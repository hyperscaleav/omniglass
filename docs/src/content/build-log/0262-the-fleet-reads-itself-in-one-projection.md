---
title: "The fleet reads itself in one projection"
date: 2026-08-19
refs: []
form: bullet
---

- **The fleet reads itself in one projection**
  ([#632](https://github.com/hyperscaleav/omniglass/issues/632)). `GET /views/fleet` returns the
  whole in-scope fleet as the canvas draws it: every location flat with its parent and recorded
  verdict, every system with its location and verdict, and one dot per component
  (`{component, verdict, primary, shared}`), never a component row. Each tier is scoped on its
  own read, so a caller who may read places but not systems gets the shape with no contents and
  an out-of-scope root is absent rather than empty. Found in review: the per-location and
  per-system verdict subqueries planned as sequential scans of the telemetry lane; the location
  arc's owner index landed with the slice (the system arc's arrived independently as #725), and
  the guard asks the planner with `enable_seqscan = off` rather than the catalog.
