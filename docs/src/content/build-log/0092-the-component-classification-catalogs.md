---
title: "The component classification catalogs"
date: 2026-07-21
refs: []
form: bullet
---

- **The component classification catalogs.** The `component_make` registry is generalized into a **`vendor`**
  catalog with a **`kind`** (`manufacturer` / `integrator` / `developer`), and two new leaf catalogs join it as
  the rest of the component-classification reference data: a **`driver`** (id, display_name, version) and a
  **`capability`** (id, display_name). Each of the three reuses the `official`-boolean chassis the type and
  property registries prove: seed-owned official rows are read-only (update / delete refused 422), a custom row
  is full CRUD gated by the resource's `<resource>:create` / `:update` / `:delete` permission (admin gains
  `<resource>:*`, the `*:read` floor gives everyone read) and audited in the same transaction, with official
  rows seeded at boot. All three ship end to end: the Storage Gateway CRUD, the Huma surface (`/vendors`,
  `/drivers`, `/capabilities`) regenerated into the OpenAPI document, the cobra CLI, and the typed SPA client,
  plus a gated **Catalog** console page each (`/vendors`, `/drivers`, `/capabilities`) with a list, detail
  blade, and create form, official rows read-only in the UI. Proven by the per-catalog CRUD integration tests
  (official read-only, the scoped permission 403), the HTTP e2e, and the console suites. This is **PR2** of the
  estate-model shift toward property / event / command + vendor / product / driver / capability / standard /
  role / health; `product`, `product_capability`, and `component.product` are the next slice
  ([ADR-0044](/architecture/decisions/#adr-0044-the-component-classification-catalogs)). The
  [core entities](/architecture/core-entities/) page stays `Partial`.
