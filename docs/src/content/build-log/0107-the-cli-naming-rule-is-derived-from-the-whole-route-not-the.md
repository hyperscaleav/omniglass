---
title: "The CLI naming rule is derived from the whole route, not the leaf noun"
date: 2026-07-22
refs: []
form: bullet
---

- **The CLI naming rule is derived from the whole route, not the leaf noun**
  ([ADR-0059](/architecture/decisions/#adr-0059-every-collection-segment-is-a-command-level)): every
  collection segment is a level, so a subresource is addressed under its owner (`component property
  list`, `principal grant create`). The old leaf-noun rule produced **24 collisions across 195
  operations** (`property list` seven ways), each silently unreachable and each patched by hand
  afterwards: `nameOverride` had reached 53 entries whose comments all described the same defect. It
  is now 14, all genuinely non-AIP `/auth` routes. The generator's grouping also became a real
  N-level tree, since bucketing by the first word and naming by the last collapsed three-word paths
  back into collisions. 67 of 202 commands renamed, 135 unchanged, zero collisions. `omniglass statu`
  (the depluralizer eating `status`) is gone. Two guards keep it: the collision test, whose
  known-collision list is now empty, and `TestDocsOnlyNameRealCommands`, which fails when a guide
  teaches a command that does not resolve and immediately found one that had never existed in any
  build plus two with no API route ([#359](https://github.com/hyperscaleav/omniglass/issues/359)).
