---
title: "The shipped seed names no real company, and a guard keeps it so"
date: 2026-10-03
refs: []
form: bullet
---

- **The shipped seed names no real company, and a guard keeps it so**
  ([#658](https://github.com/hyperscaleav/omniglass/issues/658)). The boot seed's vendors and
  products moved to the fictional omniglass-lab catalog in the system workspace slice (#795), but
  the Vendors and Products guides kept enumerating the eight real manufacturers and four real SKUs
  by hand, so the docs still claimed an affiliation the binary had already dropped. Both guides now
  render their shipped set from the generated seed facts (`<SeededSet kind="vendors" />` and
  `kind="products"`, the products table carrying each declared-property contract), the same
  generate-first render the other catalog guides use, so the claim cannot drift again. A unit test
  in `internal/seed` fails the build if any of the eight real names reappears in a shipped seed
  file; real vendors stay legal as operator data, which the guard does not touch.
