---
title: "Tag: filter the directories by their effective tags"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: filter the directories by their effective tags.** The last major tag piece, closing the
  set-and-see loop's read half: the Components, Systems, and Locations chip filters gain a single **tag**
  field that discloses one facet per tag key in use, so an operator narrows a directory by any tag through
  one guided step (**tag**, then the key, then the value) rather than a top-level field per key. A tag facet
  reads a row's **effective** value (a component matches on a tag it inherits from its system or location,
  not only a direct binding), autocompletes the values already in use for that key, and offers two new
  **value-less** operators, **is set** (`?`) and **is absent** (`!?`), that test only whether the tag is
  present. These land in the shared [`lib/predicate`](/guides/operator/fleet/#finding-one-thing) engine (an
  `exists` / `absent` `OpKey` carrying a `valueless` flag, threaded through `opsFor`, `matchOp`,
  `buildPredicate`, and `tokenToChip`) so every FilterBar inherits them, plus a `tagFilterKeys` helper that
  projects one `FilterKey` per tag key present on the loaded rows; the FilterBar keeps those presence facets
  out of the top-level field list and groups them under the `tag` entry (a direct `key:` still works as a
  fast path). `ListConfig.filterKeys` becomes **accessor-reactive** (a `FilterKeys<T>` is an array **or** an
  accessor, resolved in a reactive scope), so the facet set appears the moment the effective tags resolve and
  tracks whatever the estate is tagged with; each page merges its static facets with a `tagFacets` memo
  derived from the rows. Deferred: server-side tag filtering at pagination scale (today the match is
  client-side over the loaded rows) and a stored per-key color override
  ([#226](https://github.com/hyperscaleav/omniglass/issues/226)). Proven by a `lib/predicate` unit suite (the
  presence operators, the per-key facet projection and its dedup against the static keys, the value-less
  tokenizing) and `FilterBar` render tests (the `tag` group discloses its keys, and a `?` / `!?` token
  commits a value-less chip that renders with no value button).
