---
title: "Every component is an instance of a product"
date: 2026-08-08
refs: []
form: bullet
---

- **Every component is an instance of a product**
  ([#614](https://github.com/hyperscaleav/omniglass/issues/614)). The `component_type` registry
  returns as a tree, above the product rather than beside the component: a seeded-plus-custom
  device-class genus (`display`, `mic`, `dsp`, ...) nesting by `parent_id`, carrying the identity
  facts a generated name and a console glyph need (naming stem, icon, abbrev, default tags), each
  inheriting down the tree with override at any node ([ADR-0085](/architecture/decisions/#adr-0085-the-component_type-registry-returns-as-the-device-class-genus),
  a partial reversal of [ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)).
  `product.component_type_id` and `component.product_id` both land `NOT NULL`, closed in three
  ordered migrations (a nullable schema step, the boot seed plus a one-time backfill pointing every
  existing row at a matching generic, then the `NOT NULL` floor), so no component ever exists
  unclassified; three generic products (`generic-device`, `generic-app`, `generic-service`) cover
  anything not yet modeled more specifically. `product.kind` narrows to `device | app | service`,
  loses its silent default (required at create instead), and retires `vm`, folded into `app`
  ([ADR-0086](/architecture/decisions/#adr-0086-the-product-classification-floor-and-the-kind-split)).
  The console: the Products form gains a required Type picker over the seeded tree and an optional
  per-SKU icon override (unset resolves the type's own, walking its ancestor chain), the Components
  create form makes Product required with the three generics offered alongside every real SKU, and a
  new Types admin page (Catalog > Components) lists the registry in tree order with a minimal create
  and edit surface (a custom type's tree placement is fixed at create; there is no reparent leg yet).
  Reconciling the ADR-0047 denylist entry that banned `component_type` in docs prose (now current
  vocabulary again, in its reintroduced shape) closed out `internal/docslint`'s one carried red.
