---
title: "The system reclassify guard keys on the stem, not on the type id"
date: 2026-08-14
refs: []
form: bullet
---

- **The system reclassify guard keys on the stem, not on the type id**
  ([#706](https://github.com/hyperscaleav/omniglass/issues/706)). `UpdateSystem` re-minted a
  platform-owned name whenever the classification ID changed, and the mint does not read that id. It
  reads the STEM the type's chain resolves to (`resolveSystemTypeFacts`, inherited-first-non-null,
  ADR-0095), so two `system_type` rows inheriting one stem from a shared ancestor mint identical
  names. A reclassify between them changes no input to the mint, and re-minting anyway moved the
  name onto a lower ordinal freed by an earlier `:rename`, under `system:update` with no rename
  requested. That is the failure ADR-0101 refused for the presence-shaped guard, one step narrower.

  The fix is the sentence the component tier already carries, not a new one: `stemForSystemType` is
  the first half of `generateNameForSystemType` lifted out exactly as `stemForProduct` was lifted,
  and `systemTypeStemMoved` is `productStemMoved` over the shorter walk (a system reads its chain
  directly, a component reads a product that points at one). The regression test drives the issue's
  own reproduction: two stem-less children of one stem-carrying parent, two generated systems of the
  first type in one room, a `:rename` on the first freeing the bare name, then a reclassify of the
  second onto the sibling type. The guard-rail rides beside it, because a fix that stopped
  re-minting entirely would pass that test alone: a reclassify onto a DIFFERENT stem still re-mints,
  and an un-classify still reaches the generator and is still refused there rather than by a branch
  in the guard.

  The divergence ADR-0101 recorded between the two tiers, put there by #696/#691, closes with this:
  both now compare the resolved stem, each over its own path to one.
