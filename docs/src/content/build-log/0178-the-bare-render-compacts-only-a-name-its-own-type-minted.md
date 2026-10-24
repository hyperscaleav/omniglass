---
title: "The bare render compacts only a name its own type minted"
date: 2026-08-09
refs: []
form: bullet
---

- **The bare render compacts only a name its own type minted**
  ([#654](https://github.com/hyperscaleav/omniglass/issues/654)). `RenderBare` replaced any final
  segment ending in a digit run with the component type's `abbrev`, without checking the segment was
  one that type generated. So a component an operator renamed to `rack-3` rendered as `dsp3`, putting
  a word on a cable label that appears nowhere in the entity's name, and any hand-chosen name ending
  in digits reproduced it (`booth-2`, `row-14`). The code had drifted from its own documented design:
  both [core entities](/architecture/core-entities/) and the wire description already said the
  substitution swaps the abbrev in for the segment's **stem**. The stem was in hand the whole time,
  since `resolveTypeFacts` returns it beside the abbrev and the path attach discarded it; both now
  resolve and memoize together per distinct product, and the substitution fires only on an exact
  `<stem>-<ordinal>`. The regex that scraped a trailing ordinal out of a whole segment is gone, which
  is the first piece of the ordinal-as-a-stored-fact work
  ([#657](https://github.com/hyperscaleav/omniglass/issues/657)) landing early.
