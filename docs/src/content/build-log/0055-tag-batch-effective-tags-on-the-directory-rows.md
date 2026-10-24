---
title: "Tag: batch effective-tags on the directory rows"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: batch effective-tags on the directory rows.** The first slice of the tag-apply UI, backend only: the
  directory list routes (`GET /components`, `/systems`, `/locations`) now carry an **`effective_tags`** map (key to
  winning value, winners only) on each row, resolved for the whole page in one batched query per kind
  (`Gateway.EffectiveTags`), feeding the coming Tags column with zero per-row fetch. Effective resolution extends past
  the component: a **location** resolves the install-wide tier + its own location tree, and a **system** resolves it + its
  system tree + **the location it is placed at** ([ADR-0022](/architecture/decisions/#adr-0022-effective-tags-resolve-onto-systems-and-locations-a-placed-system-inherits-its-location)),
  so a system in a PCI building surfaces `compliance: pci`. Three per-kind recursive-CTE resolvers thread a target id
  through the ancestor chains and rank per (target, key); the resolver is scopeless by contract (the list already
  filtered the ids to the read scope, the rowActions batch pattern). Proven by a storage integration test (the
  four-band component cascade, the placed-system-inherits-location case, the non-propagating flat case, a batched
  shared-ancestor pair with no false cycle, and the empty/unknown edges) and an HTTP e2e (`effective_tags` on the
  component, system, and location list bodies). The Tags column, the type-to-add editor, and tag search follow in
  later slices ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
