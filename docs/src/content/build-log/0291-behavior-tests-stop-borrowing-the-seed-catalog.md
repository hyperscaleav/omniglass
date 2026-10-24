---
title: "Behavior tests stop borrowing the seed catalog"
date: 2026-08-26
refs: []
form: bullet
---

- **Behavior tests stop borrowing the seed catalog**
  ([#804](https://github.com/hyperscaleav/omniglass/issues/804), the #802 rename's
  structural lesson). Behavior tests mint their own products
  (`storagetest.MintProduct`, classified under the one type the test depends on)
  instead of naming seeded SKUs; the official-row refusal tests anchor on the
  generic floor rather than a brand; the seed-contract pins derive counts, role
  shapes, accepted types, and contract defaults from the embedded YAML instead of
  restating them; and a repo guard in `internal/build` fails any test file that
  names a catalog SKU outside the designated pin files, reading the SKU list from
  the seed's own render. A catalog change now touches the YAML and the pin files,
  nothing else.
