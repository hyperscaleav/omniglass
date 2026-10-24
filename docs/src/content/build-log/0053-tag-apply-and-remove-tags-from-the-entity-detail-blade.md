---
title: "Tag: apply and remove tags from the entity detail blade (TagAdder)"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: apply and remove tags from the entity detail blade (TagAdder).** The capability that closes the
  set-side of the tag console: an operator now binds and unbinds tag values from the UI, not only the CLI. Each
  component, system, and location detail blade carries a **Tags** panel listing the tags bound **directly** on the
  entity as removable colored chips, plus a staged **key -> value** add row. The key stage autocompletes the registry
  filtered by `applies_to` for the entity kind and by what is already bound (a pure `web/src/lib/tagdraft` core:
  `keySuggestions`, `exactKey`, `canCoin`, `valueValid`); with `tag:create`, a **Create key** affordance opens the
  Tags directory's create form ([#192](https://github.com/hyperscaleav/omniglass/issues/192)'s `CreateTagForm`, now
  exported) in a drawer and returns with the minted key selected. Writes are immediate (each is the entity's own
  `:setTag` / `:removeTag` write, gated by its `:update`), so there is no separate Save; the affordances hide without
  the permission. The resolved cascade (inherited tags) stays in the directory [Tags column](/guides/operator/fleet/#finding-one-thing),
  not the panel. Proven by a `tagdraft` unit suite (applies_to filtering, already-bound exclusion, exact-match and
  coin eligibility, value validity) and a `TagAdder` render test (chips, the update-gated add row and per-chip remove,
  the read-only and empty states). The full winner-plus-shadowed cascade provenance in the blade, the dynamic tag
  search facets, and a stored per-key color override are later slices ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
