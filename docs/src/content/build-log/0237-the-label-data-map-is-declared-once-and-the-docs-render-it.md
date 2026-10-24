---
title: "The label data map is declared once, and the docs render it"
date: 2026-08-14
refs: []
form: bullet
---

- **The label data map is declared once, and the docs render it**
  ([#729](https://github.com/hyperscaleav/omniglass/issues/729)). One paragraph away from the function
  table [#701](https://github.com/hyperscaleav/omniglass/issues/701) had just made generated, the keys
  a rule may READ were still typed out: in `core-entities`, in two `label_rule` API field
  descriptions, and a third time in the map literals that build them. This arc changed that map three
  times (#682 built it, #685 added the placement facts, #693 changed what `Ordinal` reports) and each
  time the prose had to be found and hand-edited.

  Each kind now declares its map ONCE, as an ordered list of key, summary and the accessor that
  produces the value, and `labelData` builds the map by ranging that declaration. The docs render
  `docs/src/generated/labeldata.json` (`cmd/labelgen` writes it beside the rule language's own
  artifact, drift-gated the same way), one table per entity kind rather than a union, because the
  differences between the three are the design: a location has no product, no vendor and no placement
  fact, and the reasons live in the declaration's own comment now instead of in three places.

  **The render is per kind and it carries a description per key**, because one key needed one. Since
  #693 `Ordinal` is not the stored ordinal: it is the number the row's NAME shows, empty when an
  operator named the row or when the mint suppressed the first of its stem, and a bare list of key
  names taught the opposite of that.

  **A live drift fell out of doing it.** The two `label_rule` descriptions, which are the console's
  field help and a row in the generated CLI reference, enumerated the map as it stood before #685: an
  operator was taught seven keys where a rule could read nine, and `LocationLabel` and
  `SystemTypeLabel`, the two the epic's worked example is built from, were missing from the surface an
  operator meets them on. A Huma description is a struct tag and cannot be built from the declaration
  at run time, so a test reads the generated OpenAPI and holds every such description to the declared
  key set and to the engine's own function names.
