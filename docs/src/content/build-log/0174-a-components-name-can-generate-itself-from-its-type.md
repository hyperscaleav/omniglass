---
title: "A component's name can generate itself from its type"
date: 2026-08-08
refs: []
form: bullet
---

- **A component's name can generate itself from its type**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 14, `3a571f1`,
  [ADR-0090](/architecture/decisions/#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)).
  Left blank on create, the platform mints `<stem>-<n>` from the resolved `component_type` chain and
  the next free ordinal among siblings sharing that stem in the placement scope, serialized by a
  transaction-scoped advisory lock keyed on stem plus scope rather than a `23505` retry. `name_generated` tracks who holds the pen: true on a
  generated create, false forever once `:rename` touches it, true again only through the new
  `:resetName` (gated by the same `component:rename` token `:rename` uses). A `:move` or a product
  reclassify recomputes a still-platform-owned name in the same transaction as the write that changed
  its inputs; an operator-typed name is never touched by either. A review pass found the generator
  trusted an unvalidated `component_type.stem` (an operator could mint a dotted or spaced stem that
  would either split under the address grammar above or violate the entity name rule outright), fixed
  by validating a stem at `component_type` write time with the same rule as a name (`77febe5`); a
  second pass found the malformed-stem fix had left the **absent** case open, an empty resolved stem
  minting the entity name `-1`, fixed by refusing an empty stem in the generator itself, requiring a
  stem on a root `component_type` (which has no ancestor to inherit one from), and validating the
  generated name as an enforced postcondition rather than an asserted comment (`49b84f6`).
