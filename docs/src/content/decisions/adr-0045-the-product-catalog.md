---
title: "The product catalog"
date: 2026-07-20
status: "Accepted"
summary: "**`product`** lands as a first-class catalog entity, the concrete SKU that binds a **`vendor`**, a **`driver`**, a **`kind`** (`device` / `app` / `service` / `vm`), and a capability set via the **`product_capability`** join; **`parent_product_id`** models variants, and **`component.product_id`** (`on delete restrict`) points a component at the SKU it is, making the product the source of a component's shape and retiring the `component_type`-as-shape notion. PR3 of the estate-model shift; consumes the vendor / driver / capability catalogs from ADR-0044"
---

- **Date:** 2026-07-20 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [Products guide](/guides/admin/products/)
- **Decision:** **`product`** is a first-class catalog entity, the concrete **SKU** (a Cisco Room Bar, a
  Samsung QM55) that ties the [ADR-0044](#adr-0044-the-component-classification-catalogs) leaf catalogs
  together. A product carries a stable `id` and `display_name`, a **`kind`** from a fixed enum (`device` /
  `app` / `service` / `vm`, default `device`, enforced by a DB CHECK and at the API edge), an optional
  **`vendor_id`** (who makes it) and **`driver_id`** (what talks to it), an optional **`parent_product_id`**
  (a self-reference: a variant points at its base product), and the `official` boolean the type and
  classification registries already use. The **capabilities** a product provides are a many-to-many set in
  the **`product_capability`** join (a video bar provides microphone, speaker, camera, codec); setting
  capabilities on an update replaces the whole set. It is a gated CRUD Catalog console page (`/products`) on
  the same chassis as the leaf catalogs: seed-owned official rows read-only (update and delete 422), custom
  rows full CRUD gated by `product:create` / `:update` / `:delete` and audited in the same transaction,
  official rows seeded at boot. Crucially, **`component.product_id`** (`on delete restrict`) now points a
  component at the product it **is**: the product is the source of a component's shape (its vendor, driver,
  and capability set), **replacing the `component_type`-as-shape notion**. The restrict FK is the referential
  guard the leaf catalogs deferred: a product still referenced by a component cannot be deleted (409). The
  vendor, driver, and parent FKs are `on delete set null` instead (deleting a vendor nulls a product's
  pointer, it does not block).
- **Context:** The estate model is shifting toward property / event / command on the signal side and vendor /
  product / driver / capability / standard / role / health on the component side.
  [ADR-0044](#adr-0044-the-component-classification-catalogs) landed vendor, driver, and capability as leaf
  catalogs with nothing to reference them; **product** is **PR3**, the layer they were built for and the first
  consumer of all three. A component's shape used to be a job for a `component_type` genus; binding a component
  to a product instead makes the shape data-driven from the SKU (the same product supplies the same vendor,
  driver, and capabilities to every component that is one), which is why `component.product` is a `restrict` FK,
  not `set null`: a product in use is load-bearing for its components.
- **Deferred:** a product's own template or field-schema binding; and the remaining component-side catalogs
  (standard, role, health).
- **Supersedes:** the `component_type`-as-shape notion (a component's shape now comes from its `product`, not
  its genus type). **Consumes** [ADR-0044](#adr-0044-the-component-classification-catalogs) (product is the
  `product` layer that ADR deferred; it references vendor, driver, and capability). One divergence from the
  leaf catalogs' prediction: their deferred delete guard lands only as the `component.product` restrict (409),
  while a product's own vendor / driver references null out (`on delete set null`) rather than blocking the
  referenced row's delete.
