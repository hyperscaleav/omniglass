---
title: "One door and the density toggle"
date: 2026-08-26
refs: []
form: bullet
---

- **One door and the density toggle**
  ([#798](https://github.com/hyperscaleav/omniglass/issues/798), stage 2 of the
  ADR-0129 reconciliation). The Inventory nav group dissolves: Fleet is the only
  way into the monitored fleet, and Nodes (infrastructure, not inventory) gets its
  own top-level entry. `FleetShell` gains the canvas/list toggle, a URL fact
  (`?view=list`): on the fleet it swaps the canvas for the classic index tables
  re-homed as kind tabs (Locations, Systems, Components: the old page components
  mounted whole, filters, blades, and creation intact, tabs permission-filtered
  like the rail entries they replace); inside a location it lists the subtree one
  row per system through the new `FleetRows`, verdict first, a row opening the
  system full screen per the altitude rule. The bare index addresses redirect to
  their kind tabs, their gates and top-bar identities surviving off-rail, and the
  command palette keeps the three as Fleet destinations.
