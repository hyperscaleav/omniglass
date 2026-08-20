---
title: Products
description: "The Products catalog: a concrete SKU binding a vendor, a driver, and a kind, classified under a component_type; a component points at the product it is; official rows read-only, admin-gated custom ones."
---

**Catalog, under Components: Products** (`/products`, with `product:read`, covered by every viewer's `*:read` floor)
is the directory of **products**: the concrete SKUs the fleet is built from, on the same
flat-registry pattern as [Location Types](/guides/admin/location-types/) and [Tags](/guides/admin/tags/). A product is
a specific model (a **Cisco Room Bar**, a **Samsung QM55**), not an organization and not an installed
unit. It is where the two leaf catalogs converge: the [vendor](/guides/admin/vendors/) that makes
it and the [driver](/guides/admin/drivers/) that speaks to it, classified by a **kind** and a
**component type**. Each row shows the
**name** (for example `cisco-room-bar`), the **label**, its
**vendor**, **driver**, **kind**, and its **origin** (**official** or **custom**). A
product also carries an `id`, a uuid minted by the database, the internal address the handle resolves
to ([ADR-0062](/architecture/decisions/)); the handle is what you type and read.

A product is also what a **component** points at: `component.product_id` names the product a component
**is**, required, so every component always resolves a shape (its vendor, driver, and classification)
and, through its product, a device-class **[component type](/architecture/core-entities/#catalog-reference-data-component_type)**. The
component-level `component_type` retired in the fields fold; the registry returned differently shaped,
above the product rather than beside the component, so a component's genus is read through the product
it is, never stored on the component itself. The system side has the same arrangement one level up: a
system conforms to a [standard](/guides/admin/standards/), which is the blueprint's counterpart of a
product. See [core entities](/architecture/core-entities/) for where both registries sit in the fleet
model.

- **Kind** classifies what the product is: a **device** (a physical unit), an **app** (software), or a
  **service** (something hosted). There is no default: kind is required at create, so a mislabeled
  cloud service never reads as correct through a silent fallback. It is a closed set; a value outside
  it is refused (422), and the retired **vm** value folds into **app** (nothing forked on a virtual
  machine that did not fork the same way on any other app).
- **Type** classifies the product under a [component type](/architecture/core-entities/#catalog-reference-data-component_type) (required):
  the device-class genus (`display`, `dsp`, `mic`, ...) that carries the naming stem a generated
  component name uses, the console glyph, and the hostname abbreviation. Three generic types
  (`generic-device`, `generic-app`, `generic-service`) exist for anything not yet modeled more
  specifically. It is also what a
  [role assignment](/guides/admin/standards/#staff-a-system-against-its-standard) checks: a
  component fills a slot only when its product's type falls within a type the role accepts (self or
  a descendant), and, if the role pins specific products, only when its product is one of them. That
  guard runs once, at assignment; afterward the occupant keeps its slot unless its own health
  verdict goes to outage (a lesser alarm degrades it but does not cost it the slot); see
  [Work with an entity](/guides/operator/entities/#roles-on-a-system).
- **Icon** is an optional override on the type's icon: unset, a product's glyph is whatever its type
  resolves to (walking up the type's own tree if the type itself leaves its icon blank); set, the
  override wins. A per-SKU glyph is the exception, not the rule.
- **Vendor**, **driver**, and **parent** are each optional pointers: the vendor that makes the product,
  the driver that talks to it, and a **parent product** it is a variant of (see below). Each must
  resolve against the vendor / driver / product catalogs; an unknown reference is refused (422). These
  three are nulled if their target is deleted, not blocked: removing a vendor clears the product's
  vendor pointer rather than blocking the vendor delete.
- **Variants** use **parent product**: a specific SKU that inherits from a base product points at it
  with `parent_product_id` (a trim or regional variant of the same model). A product with no parent is
  a base product.
- **New product** (with `product:create`, an admin permission) opens a create drawer: give it a
  **name** (unique tenant-wide, e.g. `cisco-room-bar`) and a **label**, pick
  its **kind** (defaults to device) and its **component type**, and, optionally, its **vendor**,
  **driver**, and **parent product**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `product:update`) edits the
  label, vendor, driver, kind, type, and parent; the **name** is fixed, since a catalog
  row carries no rename. **Delete** (with `product:delete`) removes the row, behind a confirm. A
  verb you lack greys just that button, its hover reason naming the permission
  (`Requires product:update`, `Requires product:delete`); the pair never disappears.
- An **official** row is always read-only: the blade keeps the Edit and Delete pair in place,
  greyed, with the reason on hover: "Official: ships with Omniglass and updates with it."
  Omniglass ships a starter set of official products (Cisco Room Bar, Samsung
  QM55, Shure MXA920, Crestron TSS-1070), upserted idempotently at boot so the shared set cannot drift
  install to install; add a custom product for anything else.
- **Delete** enforces the referential guard the leaf catalogs deferred: a product still referenced by a
  **component** (`component.product_id`) cannot be deleted (409), the same delete-refused-while-referenced
  rule the [Location Types](/guides/admin/location-types/) registry enforces. Remove or repoint the component first. An
  official row is still refused (422) regardless.

Minting a product is admin-gated, and the product form is where the vendor and driver
catalogs are finally consumed, as the pickers that choose a product's vendor and driver. The same
operations are `omniglass product list/get/create/update/delete` from the CLI
(see the [CLI reference](/reference/cli/)).

## Declared properties: the product's contract

A product's blade also carries a **Declared properties** panel, the product's **contract**: which
[properties](/guides/admin/properties/) every instance of the product exposes, and what each one
defaults to. It is half of "the product is the source of a component's shape": the type says what
the product **is**, the contract says what it **carries**.

- **Declare a property** (with `product:update`) picks a name from the property catalog, optionally
  types a **default**, and optionally marks it **required**. The property must already exist in the
  catalog, since the contract only names it: mint it under
  [Catalog, under Telemetry: Properties](/guides/admin/properties/) first. Declaring is **idempotent**, so declaring a
  property already on the contract revises that line in place rather than adding a second.
- **The default is typed by the catalog, not here.** The panel labels the input with the property's
  data type, coerces what you type to it, and refuses a value that will not parse. Type and validation
  live on the property, so a product cannot redefine what `serial-number` means, only what a fresh
  instance of that product starts with.
- **Required** means an instance must resolve the property to a value. A component of the product
  cannot save with a required property empty (see
  [set a property on an instance](/guides/admin/properties/#set-a-property-on-an-instance)).
- **Withdraw** (with `product:delete`, from the blade's edit mode, so the pencil's `product:update` is also in the path; behind a confirm) removes a line from the contract. Components
  **keep** any value they set for it; the value simply reads as **off contract** from then on, since
  nothing declares it any more.
- An **official** product's contract is read-only, like the rest of the row: the shipped
  Cisco Room Bar and Samsung QM55 declare `serial-number`, `firmware-version`, and
  `model-number`, and those declarations come with the release.

From the CLI the contract is `omniglass product property list <id>`,
`omniglass product property update <id> <property>`, and
`omniglass product property delete <id> <property>`.
