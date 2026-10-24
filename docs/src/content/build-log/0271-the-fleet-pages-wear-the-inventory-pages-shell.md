---
title: "The fleet pages wear the inventory pages' shell"
date: 2026-08-19
refs: []
form: bullet
---

- **The fleet pages wear the inventory pages' shell**
  ([#630](https://github.com/hyperscaleav/omniglass/issues/630)). Ruled after a look at the
  Locations list: every zoom shares one layout, `FleetShell`, taken from `TreeList`'s summary
  rail (badges that expand to a tile board with a verdict donut and facet legend, every badge
  a filter toggle, open state persisted per page) over the console's `ListShell` (filter bar
  and card) around the zoom's own body. The summary is fleet-wide at every zoom; right-hand
  drawers exist only as detail blades. The tiles the previous cut had introduced are folded
  into the rail's badges and board (#630).
