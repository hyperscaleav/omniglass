---
title: "The type resource renames to location_type"
date: 2026-08-06
status: "Accepted"
summary: "the permission resource type renames to location_type on every surface (route stamps, roles seed, console gates, guard fixtures); the generic word retires from the permission vocabulary"
---

- **Date:** 2026-08-06 | **Status:** Accepted | **Pages:**
  [identity and access](/architecture/identity-access/), [API](/architecture/api/)
- **Decision:** the permission resource `type` renames to `location_type` on every surface: the
  route stamps, the roles seed, the console gate strings, and the authz guard fixtures. The
  location-type property contract routes follow the same resource (`location_type:update` declares,
  `location_type:delete` withdraws). The generic word `type` retires from the permission
  vocabulary; no route stamps it and no role grants it.
- **Context:** one word hid two registries: the Types console page held location types and secret
  types behind one nav word while `type:*` gated only the location registry, and the contract
  routes gating `type:*` beside `product` and `standard` gating their own nouns was a live
  asymmetry. The same one-word-hides-two-things shape the catalog arc exists to end, renamed
  pre-release while the rename is cheap; the Types page split carried it (#598, the epic #601).
