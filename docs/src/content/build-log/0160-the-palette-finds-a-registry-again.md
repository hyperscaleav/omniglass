---
title: "The palette finds a registry again"
date: 2026-08-07
refs: []
form: bullet
---

- **The palette finds a registry again**
  ([#628](https://github.com/hyperscaleav/omniglass/issues/628)). Collapsing the catalog rail
  took the registries out of the ⌘K palette with it: its command list was flattened from the
  nav items alone, so once Products, Metrics, and Tags moved off the rail, typing their names
  returned no matches and the only catalog destination left was the word Catalog. The palette
  now builds its list from both surfaces' own tables, the nav items through the same
  `filterNav` that hides a sidebar button and the catalog groups through the same
  `visibleGroups` the subrail and the Overview cards render from, so a registry is findable by
  name without a second membership list to drift, tagged `Catalog · <group>` and searchable by
  its group (`telemetry` lists Metrics, Properties, and Events). Gating came with it: the list
  was previously built at module scope from the unfiltered nav, offering an Audit jump the
  route guard would only bounce, and it is now judged per caller like every other surface. The
  pathless soon slots (the Templates reservations) are no destination and stay out; secret
  types keeps its standing ruling of no nav slot.
