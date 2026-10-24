---
title: "The effective-values resolution view"
date: 2026-07-21
refs: []
form: bullet
---

- **The effective-values resolution view**, resolved per membership: the component detail gains an
  **Effective tags** panel showing, per key, the **winning value**, the tier it won from (global,
  location, system, or the component itself) and the owner's name, with the **shadowed candidates**
  struck through beneath it. `GET /components/{name}/effective-tags` had existed for some time with no
  console consumer, so the provenance it returns was being thrown away and the list's flat pills were
  the only view of a resolved value. A **system selector** appears only when the component belongs to
  more than one system, and switching it re-resolves against that membership (`?system=`), so a shared
  device visibly answers `prod` for one room and `lab` for another. A component in a single system
  sees no selector and reads exactly as before. This closes epic
  [#324](https://github.com/hyperscaleav/omniglass/issues/324): the engine became per-membership in
  slice 2, and this is the surface that teaches it. [Cascade](/architecture/cascade/) and
  [core entities](/architecture/core-entities/) stay `Partial`.
