---
title: "effective tags resolve onto systems and locations; a placed system inherits its location"
date: 2026-07-13
status: "Accepted"
summary: "Directory rows carry batch-resolved effective tags; effective resolution extends to systems and locations, and a placed system inherits its location's tags"
---

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/)
- **Decision:** The directory **Tags column** shows a row's **effective** (resolved-cascade) tags, not its direct
  bindings, so the list routes (`GET /components`, `/systems`, `/locations`) carry an **`effective_tags`** map (key to
  winning value, winners only) per row, resolved for the whole page in **one batched query per kind**
  (`Gateway.EffectiveTags(kind, ownerIDs)`, three per-kind recursive-CTE resolvers that thread a target id through the
  ancestor chains and rank per `(target, key)`). This required **defining effective tags for systems and locations**,
  which previously only components resolved: a **location** resolves `global` plus its own location tree; a **system**
  resolves `global`, its own system tree, **and the location it is placed at** (its `location_id` tree). A placed
  system therefore inherits its location's tags (a system in a PCI building surfaces `compliance: pci`), consistent
  with how a component picks up its own `location_id`. A component is unchanged (the full four-band arc). The resolver
  is **scopeless by contract**: the list query has already filtered the ids to the caller's read scope, so the batch
  adds no per-id check, matching the existing `rowActions` batch. Winners only in the column; provenance (which scope a
  value came from) stays in the per-entity effective-tags detail view.
- **Context:** The tag-apply UI needs each directory row to show what tags actually apply to it. The cheaper option was
  to embed a row's **direct** bindings (a flat, non-recursive `where owner_id = any($1)` lookup); the architect chose
  **effective** so the column reflects inherited values, not just locally-set ones. That choice moved real work to the
  backend (a batched recursive cascade versus a flat index scan) and forced the systems-and-locations effective
  definition, whose one genuine call was whether a **system inherits its location**: yes, because a system carries a
  `location_id` exactly as a component does, so treating it as placement-that-inherits is the consistent reading. The
  added cost is a small bounded per-row recursion over the shallow estate trees, one round-trip, and is capped by the
  directory page size. This is the first (backend) slice of the tag-apply UI; the Tags column, the type-to-add editor,
  and tag search consume it in later slices.
- **Closes:** issue [#201](https://github.com/hyperscaleav/omniglass/issues/201) (batch effective-tags resolver);
  part of [#189](https://github.com/hyperscaleav/omniglass/issues/189).
