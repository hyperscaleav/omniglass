---
title: "The standard declares a map, the system renders it"
date: 2026-08-26
refs: []
form: bullet
---

- **The standard declares a map, the system renders it**
  ([#791](https://github.com/hyperscaleav/omniglass/issues/791), ADR-0128). One jsonb
  value on `standard` (aspect plus normalized 1-based role positions, validated in Go,
  cleared by JSON null), carried on the standard wire and patched through the standard
  API; both example standards declare one in the dev seed. The system zoom grows its
  first second facet: the Map tab (the `TabRail` mounts), a top-down room with one marker
  per declared position of the build in use, solid in the occupant's state and linking to
  its leaf, hollow where nobody is staffed.
