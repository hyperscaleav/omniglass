---
title: "A re-mint follows the mint's inputs, not a field's presence"
date: 2026-08-12
refs: []
form: bullet
---

- **A re-mint follows the mint's inputs, not a field's presence**
  ([#696](https://github.com/hyperscaleav/omniglass/issues/696),
  [#691](https://github.com/hyperscaleav/omniglass/issues/691),
  [ADR-0101](/architecture/decisions/#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests)
  amended). The component tier catches up with the system tier on the two guards that decide whether
  a platform-owned name is minted again. `MoveComponent` re-minted whenever the row was
  platform-named, so a `:move` that re-stated the location the component already sat at moved its
  name; `UpdateComponent` re-minted whenever the `product` field was PRESENT in the patch, so a save
  that re-stated the product did the same. Neither is a harmless recompute, because allocation is
  lowest-free: an ordinal freed by an earlier `:rename` means the re-mint hands `display-2` the name
  `display-1`, under `component:move` or `component:update`, with no rename requested and possibly no
  `component:rename` grant held. The label follows the name (it reads the ordinal), so the silent
  rename silently relabelled too.

  The move guard is the system tier's, unchanged: the placement **bucket** compared as a `nameScope`
  value rather than as two pointers, because a parent wins over a location and a parented component
  that merely relocates has moved a pointer and not a bucket. The reclassify guard is deliberately
  NOT the system tier's. A system reads its stem from a `system_type` chain; a component reads it
  from a product that points at a `component_type` chain, one hop further, so two products under one
  type mint identical names and a product-id comparison would still move the name on a real
  reclassify. It compares the resolved **stem**, with `stemForProduct` lifted out of
  `generateNameForProduct` so a guard can ask what a name would be minted from without minting one.
  The matching residual one tier up (two `system_type` rows inheriting one stem) is
  [#706](https://github.com/hyperscaleav/omniglass/issues/706).

  Six tests pin both directions on both verbs, since a guard that suppresses too much is the same
  defect from the other side: the two no-op acts, the parented relocate a pointer comparison gets
  wrong, the same-stem reclassify a product-id comparison gets wrong, and the two real acts that must
  still re-mint with the label following. No existing expectation moved.
