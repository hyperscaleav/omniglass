---
title: Products
description: "The Products catalog: a concrete SKU binding a vendor, a driver, a kind, and the capabilities it provides; a component points at the product it is; seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Products** (`/products`, with `product:read`, covered by every viewer's `*:read` floor)
is the directory of **products**: the concrete SKUs the estate is built from, on the same
flat-registry pattern as [Types](/guides/admin/types/) and [Tags](/guides/admin/tags/). A product is
a specific model (a **Cisco Room Bar**, a **Samsung QM55**), not an organization and not an installed
unit. It is where the three leaf catalogs converge: the [vendor](/guides/admin/vendors/) that makes
it, the [driver](/guides/admin/drivers/) that speaks to it, and the
[capabilities](/guides/admin/capabilities/) it provides, classified by a **kind**. Each row shows the
**id**, the **display name**, its **vendor**, **driver**, **kind**, and its **origin** (**official**,
seed-owned, or **custom**).

A product is also what a **component** points at: `component.product_id` names the product a component
**is**, and the product supplies that component's shape (its vendor, driver, and capability set). This
replaces the old `component_type`-as-shape notion: a component's shape comes from its product now, not
a separate genus. See [core entities](/architecture/core-entities/) for where the product registry
sits in the estate model.

- **Kind** classifies what the product is: a **device** (a physical unit), an **app** (software), a
  **service** (something hosted), or a **vm** (a virtual machine). It defaults to **device** and is a
  closed set; a value outside it is refused (422).
- **Vendor**, **driver**, and **parent** are each optional pointers: the vendor that makes the product,
  the driver that talks to it, and a **parent product** it is a variant of (see below). Each must
  resolve against the vendor / driver / product catalogs; an unknown reference is refused (422). These
  three are nulled if their target is deleted, not blocked: removing a vendor clears the product's
  vendor pointer rather than blocking the vendor delete.
- **Capabilities** is the set of things the product provides (a room bar provides **microphone**,
  **speaker**, **camera**, **codec**), each chosen from the [capability](/guides/admin/capabilities/)
  catalog. It is a many-to-many set: a product declares as many as it needs, and setting capabilities
  on an update **replaces** the whole set. An unknown capability id is refused (422).
- **Variants** use **parent product**: a specific SKU that inherits from a base product points at it
  with `parent_product_id` (a trim or regional variant of the same model). A product with no parent is
  a base product.
- **New product** (with `product:create`, an admin permission) opens a create drawer: name its **id**
  (a short identifier, unique tenant-wide, e.g. `cisco-room-bar`), give it a **display name**, pick its
  **kind** (defaults to device), and, optionally, its **vendor**, **driver**, **parent product**, and
  **capabilities**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `product:update`) edits the
  display name, vendor, driver, kind, parent, and capabilities; the id is fixed. **Delete** (with
  `product:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships a starter set of official products (Cisco Room Bar, Samsung
  QM55, Shure MXA920, Crestron TSS-1070), upserted idempotently at boot so the shared set cannot drift
  install to install; add a custom product for anything else.
- **Delete** enforces the referential guard the leaf catalogs deferred: a product still referenced by a
  **component** (`component.product_id`) cannot be deleted (409), the same delete-refused-while-referenced
  rule the [Types](/guides/admin/types/) registry enforces. Remove or repoint the component first. An
  official row is still refused (422) regardless.

Minting a product is admin-gated, and the product form is where the vendor, driver, and capability
catalogs are finally consumed, as the pickers that choose a product's vendor and driver and declare its
capabilities. The same operations are `omniglass product list/get/create/update/delete` from the CLI
(see the [CLI reference](/reference/cli/)).
