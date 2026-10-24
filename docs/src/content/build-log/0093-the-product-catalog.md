---
title: "The product catalog"
date: 2026-07-21
refs: []
form: bullet
---

- **The product catalog.** **`product`** lands as the concrete **SKU** that ties the
  [ADR-0044](/architecture/decisions/#adr-0044-the-component-classification-catalogs) leaf catalogs together:
  a **`kind`** (`device` / `app` / `service` / `vm`), an optional `vendor_id` (who makes it) and `driver_id`
  (what talks to it), an optional `parent_product_id` (a variant points at its base product), the `official`
  boolean, and the capabilities it provides through the **`product_capability`** join (a video bar provides
  microphone, speaker, camera, codec; a replace-the-whole-set update). It reuses the `official`-boolean
  chassis: seed-owned official rows read-only (update / delete 422), custom rows full CRUD gated by
  `product:create` / `:update` / `:delete` (the `*:read` floor gives everyone read) and audited in the same
  transaction, official rows seeded at boot. The slice also adds **`component.product_id`**
  (`on delete restrict`), the pointer from a component to the product it **is**, making the product the source
  of a component's shape and retiring the `component_type`-as-shape notion; a product still referenced by a
  component cannot be deleted (409). It ships end to end: the Storage Gateway CRUD, the Huma surface
  (`/products`) regenerated into the OpenAPI document, the cobra CLI (`omniglass product
  list/get/create/update/delete`), the typed SPA client, and a gated **Catalog** console page (`/products`)
  with a list, detail blade, and create form (vendor, driver, parent, and capability pickers), official rows
  read-only in the UI. Proven by the product CRUD integration test (official read-only, the `product:create`
  403, an unknown vendor / driver / capability reference 422, and the in-use 409 when a component points at
  it), the `/products` HTTP e2e, and the console suite. This is **PR3** of the estate-model shift toward
  property / event / command + vendor / product / driver / capability / standard / role / health
  ([ADR-0045](/architecture/decisions/#adr-0045-the-product-catalog)). The
  [core entities](/architecture/core-entities/) page stays `Partial`.
