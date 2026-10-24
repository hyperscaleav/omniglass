---
title: "One blade per fleet kind"
date: 2026-08-26
refs: []
form: bullet
---

- **One blade per fleet kind**
  ([#799](https://github.com/hyperscaleav/omniglass/issues/799), stage 3 of the
  ADR-0129 reconciliation). `EntityBlade` renders each fleet kind as a condensed cut
  of its workspace cores: verdict and since lead, the alarms say why, the 30-day
  strip rides under them, a system lists its members and room vitals, a location its
  subtree rollup and what needs attention. Every body self-fetches by id, so the one
  `fleetRegistry` serves the fleet canvas, the system zoom (whose member cards,
  alarm links, and map markers now open the component blade instead of leaving the
  page, per the altitude rule), and the index tables (TreeList grows a
  `bladeOverride` seam that retires the inventory-era detail blade for these kinds).
  Expand promotes to the identity route; the label edits in place through the
  footer pencil and Delete confirms, both gated by the row's scope-aware actions.
