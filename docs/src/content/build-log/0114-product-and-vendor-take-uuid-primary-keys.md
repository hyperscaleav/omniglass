---
title: "`product` and `vendor` take uuid primary keys"
date: 2026-07-22
refs: []
form: bullet
---

- **`product` and `vendor` take uuid primary keys**
  ([ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle),
  slice 1 of [#262](https://github.com/hyperscaleav/omniglass/issues/262)): their kebab id becomes `name`, a
  unique **renameable** handle, and five inbound foreign keys move to the uuid (`product.vendor_id`,
  `product.parent_product_id`, `product_capability`, `product_property`, and `component.product_id`). The
  registries were the last place a foreign key pointed at a mutable string, so a product id could not be
  corrected after a typo or a rebrand. The API now carries **both** forms on every body and accepts either
  wherever a product or vendor is named. Proven by `TestRegistryHandleRenameKeepsReferences`, which renames
  both handles under a live sub-product, contract, capability set, and component instance, and asserts every
  reference resolves and reads the new handle; **mutation-checked**. The remaining seven registries follow,
  and five of those are a **rename** of `id` to `name` rather than an addition, since the registries did not
  agree with each other on the column name to begin with.
