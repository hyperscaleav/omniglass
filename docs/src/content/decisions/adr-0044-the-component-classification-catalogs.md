---
title: "The component classification catalogs"
date: 2026-07-20
status: "Accepted"
summary: "The `component_make` catalog is generalized into **`vendor`** (a `kind` of manufacturer / integrator / developer), and two new leaf catalogs join it, **`driver`** (id, display_name, version) and **`capability`** (id, display_name), as the component-classification reference data: each a gated CRUD Catalog console page with read-only official seeded rows. `product` + `product_capability` + `component.product` are the next slice. This is PR2 of the estate-model shift toward property / event / command + vendor / product / driver / capability / standard / role / health"
---

- **Date:** 2026-07-20 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/)
- **Decision:** The [`component_make`](#adr-0031-component_make-registry-slice-1-an-official-boolean-a-deferred-referential-guard-and-website-scheme-validation)
  catalog is generalized into a **`vendor`** catalog carrying a **`kind`** (`manufacturer` / `integrator` /
  `developer`), so the one organization that makes, integrates, or writes for a component is a single
  reference entity rather than a make-only registry. Two new **leaf** catalogs join it as the rest of the
  component-classification reference data: a **`driver`** (`id`, `display_name`, `version`, the software that
  speaks to a component) and a **`capability`** (`id`, `display_name`, a thing a component can do). Each of the
  three is a **gated CRUD Catalog console page** (`/vendors`, `/drivers`, `/capabilities`), reusing the
  `official`-boolean chassis the type and property registries already use: seed-owned official rows are
  read-only (an official row refuses update and delete, 422), a custom row is full CRUD gated by the
  resource's `<resource>:create` / `:update` / `:delete` permission and audited in the same transaction. The
  official rows are seeded at boot. This is a pure classification slice: **`product`** (the specific model an
  organization sells), the **`product_capability`** link, and the **`component.product`** pointer that binds a
  component to its product are the **next slice**, not this one.
- **Context:** The estate model is shifting from the make/model catalog sketch toward a fuller classification
  vocabulary: **property / event / command** on the signal side (property landed in
  [ADR-0043](#adr-0043-the-property-catalog)) and **vendor / product / driver / capability / standard / role /
  health** on the component side. This is **PR2** of that shift. `component_make` was manufacturer-only, but the
  same organization concept covers an integrator who installs the estate and a developer who writes a component's
  software, so generalizing make into a `kind`-tagged vendor is the honest widening rather than three parallel
  organization registries. Driver and capability are leaf catalogs (no tree, no cross-references yet), so they
  ship as the plain seeded-plus-CRUD pattern the registries already prove; they gain their bindings (a driver to
  an interface / product, a capability to a product) when the product slice gives them something to reference.
- **Deferred:** `product`, `product_capability`, and `component.product` (the next slice); a referential delete
  guard on vendor / driver / capability (nothing references them yet, exactly as `component_make` shipped with no
  guard until `component_model` was to land); and an operator shadow of an official row.
- **Refines:** [ADR-0031](#adr-0031-component_make-registry-slice-1-an-official-boolean-a-deferred-referential-guard-and-website-scheme-validation)
  (the `component_make` registry is renamed and generalized to `vendor` with a `kind`; its `official`-boolean,
  deferred-delete-guard, and website-scheme-validation calls carry over unchanged).
