---
title: "The console's visualizations are inventoried for the widget canvas (#1827)"
date: 2026-11-15
refs: [1827, 1826]
---

Epic #1826 rebuilds the console's charts, cards, pills and lists as widgets on a canvas, and
needed to know what it was replacing. The new [visualization inventory](/contributing/visualization-inventory/)
lists 49 places the console draws one, each with its component, the routes that show it, the data
primitive, the API read behind it, how live it is, and the widget it maps to, and checks all 51
pages under `web/src/pages/`. It found that `TimeseriesChart` is the only chart and appears on two
pages, that `propertySeries` and `propertyBuckets` have no caller (so the property distribution and
property timeline widgets have nothing to migrate), that a component's own view shows latest values
but no history, and that more than a quarter of the surfaces are verdict pills in different frames. It is a
point-in-time survey at `e953d9bf`, docs only, with no code changed.
