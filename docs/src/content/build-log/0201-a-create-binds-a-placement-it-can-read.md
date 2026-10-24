---
title: "A create binds a placement it can read"
date: 2026-08-08
refs: [700]
form: bullet
---

- **A create binds a placement it can read** ([#700](https://github.com/hyperscaleav/omniglass/issues/700),
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)
  amended). `CreateComponent` and `CreateSystem` resolved their `location` and `system` references
  existence-only, and so did the two `:move` verbs. That was defensible while a placement reference
  was only a pointer. It stopped being defensible when
  [ADR-0100](/architecture/decisions/#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)
  put placement into the label data map: the label these four writes stamp is rendered from the
  location's own label and the primary system's TYPE label, and the row comes straight back in the
  response, so naming a location you cannot read handed you its label. The
  [draft-render route](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)
  built beside it is what made the gap visible: the console's preview was **stricter than the create
  it previews**, so an operator could be refused a draft for a placement and then create into it
  anyway.

  All four now resolve the placement through one seam, `resolvePlacementRef`, in the caller's
  `location:read` / `system:read` scope, which is the scope `:renderLabel` already injects for the
  same references. The refusal is the read path's **non-disclosing not-found** rather than the write
  path's 403: a status that separated "no such location" from "a location you may not see" would be
  the same disclosure one level up, and sharing one refusal is what keeps the preview and the create
  from disagreeing. The moves are in scope for the same reason the creates are, since a relocate
  restamps the label from the DESTINATION. The reclassify paths are not: `UpdateComponent` and
  `UpdateSystem` resolve only catalog rows (product, standard, system_type), which are not scoped
  trees, and touch no placement reference at all.

  Two consequences are stated rather than left to be discovered. A caller holding no grant on a tier
  now binds nothing on it, because it reads nothing on it: while the cross-tier scope expansion is
  unbuilt (#10), a component-scoped grant confers no `location:read`, so such a caller can no longer
  place a component at any location. That is the same sentence the read side already said, and the
  draft route already enforced. And the ambiguity redaction these binds carried (`withoutCandidates`)
  is now belt and braces rather than the load-bearing guard it was, since narrowing to the caller's
  read scope means a candidate list can only name rows that caller may read; making it useful again
  is [#697](https://github.com/hyperscaleav/omniglass/issues/697).
