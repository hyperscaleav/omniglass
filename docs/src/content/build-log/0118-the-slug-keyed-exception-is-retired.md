---
title: "The slug-keyed exception is retired"
date: 2026-07-22
refs: []
form: bullet
---

- **The slug-keyed exception is retired** (slice 5 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle),
  the epic's close): a docs-and-guard slice with one real fix. The [api-first](/contributing/api-first/) rule
  now reads "every foreign key stores a uuid, no exception"; [ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)'s
  slug-keyed carve-out is marked retired. The `TestReferencesCarryBothForms` guard drops its slug-keyed
  allow-list: the registry references (`product_id`, `vendor_id`, `driver_id`, `standard_id`, and the two
  parents) move into the both-forms rule, and emptying the allow-list **surfaced a real gap**, the
  component response carried `product_id` without the product's name, now fixed by adding the `product`
  handle to the component body (the guard's whole purpose, catching a uuid-only reference). The storage
  helper collapses in step: the per-registry `productRefCol` / `vendorRefCol` and the `registryHandles`
  set fold into one `registryRefCol(ref)`, since every registry behaves identically now. **Epic #262 is
  complete**: no foreign key anywhere points at a mutable string.
