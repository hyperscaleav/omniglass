---
title: "Tag: the colored effective-tags column on the directories"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: the colored effective-tags column on the directories.** The first VISIBLE tag surface, consuming the
  batch resolver: Components, Systems, and Locations each gain a **Tags** column rendering their **effective**
  tags (the resolved cascade winners, not just direct bindings) as `key = value` pills. Color is
  **client-derived** and needs no backend: `tagHue(key)` (a pure `web/src/lib/tagcolor` FNV-1a hash into a
  curated, yellow-green-pruned 12-hue ramp) maps a key to a stable hue, so the same key is the same color
  everywhere; a shared `TagPills` component crosses only that hue into a new unlayered `.tag-pill` CSS recipe
  (text and outline are the seed, the fill is the seed at 15% via `color-mix`), with lightness and chroma in
  per-theme tokens so one hue reads correctly and stays contrast-safe in both themes. The chips stay on one
  line, fading at the edge when they overflow, and a portaled hover tooltip reveals the full wrapped set (a `wrap`
  prop on `TagPills` is the seam for a future per-table wrap toggle). The column registers in each
  page descriptor, so the columns menu shows, hides, and reorders it, and it sorts by key set. A stored per-key
  color override is a later slice; the type-to-add editor and tag search follow ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
  Proven by a `tagHue` determinism unit test (stable, in-ramp, spread) and a `TagPills` render test (sorted
  chips, the `--tag-h` per key, the empty dash), plus the page-descriptor conformance matrix.
