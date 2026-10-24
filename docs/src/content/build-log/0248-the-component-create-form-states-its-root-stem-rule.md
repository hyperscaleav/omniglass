---
title: "The component create form states its root-stem rule"
date: 2026-08-14
refs: []
form: bullet
---

- **The component create form states its root-stem rule**
  ([#744](https://github.com/hyperscaleav/omniglass/issues/744)). One rule, refused on both tiers
  (`ErrRootComponentTypeNeedsStem` and `ErrRootSystemTypeNeedsStem`): a root type has no ancestor to
  take a stem from, so it must state one. The SYSTEM create form had said so since it shipped; the
  COMPONENT one never had, so the same operator was warned on one page and met the constraint as a
  422 after submitting on the other. Pre-existing, found by #742's copy sweep.

  The wording is matched rather than invented, and then written ONCE: the shared tail
  (`Leave blank to inherit the parent's; required on a root.`) and the shared Parent sentence live in
  `lib/catalog` beside `OFFICIAL_LOCK`, so the two forms cannot state the same rule differently
  again. Each form still leads with its own fact, since a component's stem and a system's are
  different facts. Both pages assert the full strings verbatim, so a change to either has to fail
  two tests.

  The paired-form sweep the issue asked for found nothing else: only these two registries are
  tree-shaped with a stem, `location_type` is flat and carries no stem column at all, and the abbrev
  and icon hints already agreed about inheritance on both pages.
