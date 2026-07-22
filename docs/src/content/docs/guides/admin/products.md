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
a separate genus. The system side has the same arrangement one level up: a system conforms to a
[standard](/guides/admin/standards/), which is the blueprint's counterpart of a product. See
[core entities](/architecture/core-entities/) for where the product registry sits in the estate model.

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
  on an update **replaces** the whole set. An unknown capability id is refused (422). This set is the
  **default** for the product's components, not the last word: a component
  [adds or suppresses capabilities](/guides/admin/capabilities/#what-a-component-actually-provides) of
  its own over it, and the resolved set is what a
  [role assignment](/guides/admin/standards/#staff-a-system-against-its-standard) is checked against.
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

## Declared properties: the product's contract

A product's blade also carries a **Declared properties** panel, the product's **contract**: which
[properties](/guides/admin/properties/) every instance of the product exposes, and what each one
defaults to. It is the second half of "the product is the source of a component's shape": capabilities
say what the product can **do**, the contract says what it **carries**.

- **Declare a property** (with `product:update`) picks a name from the property catalog, optionally
  types a **default**, and optionally marks it **required**. The property must already exist in the
  catalog, since the contract only names it: mint it under
  [Catalog > Properties](/guides/admin/properties/) first. Declaring is **idempotent**, so declaring a
  property already on the contract revises that line in place rather than adding a second.
- **The default is typed by the catalog, not here.** The panel labels the input with the property's
  data type, coerces what you type to it, and refuses a value that will not parse. Type and validation
  live on the property, so a product cannot redefine what `serial_number` means, only what a fresh
  instance of that product starts with.
- **Required** means an instance must resolve the property to a value. A component of the product
  cannot save with a required property empty (see
  [set a property on an instance](/guides/admin/properties/#set-a-property-on-an-instance)).
- **Withdraw** (with `product:delete`, behind a confirm) removes a line from the contract. Components
  **keep** any value they set for it; the value simply reads as **off contract** from then on, since
  nothing declares it any more.
- An **official** (seed-owned) product's contract is read-only, like the rest of the row: the seeded
  Cisco Room Bar and Samsung QM55 ship declaring `serial_number`, `firmware_version`, and
  `model_number`, and those declarations come with the release.

From the CLI the contract is `omniglass product property list <id>`,
`omniglass product property update <id> <property>`, and
`omniglass product property delete <id> <property>`.
