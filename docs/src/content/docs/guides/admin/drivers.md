---
title: Drivers
description: "The Drivers catalog: the implementations that get, emit, and set a product's signals (name, label, version), official rows read-only, admin-gated custom ones."
---

**Catalog, under Components: Drivers** (`/drivers`, with `driver:read`, covered by every viewer's `*:read` floor)
is the directory of **drivers**: the implementation that gets, emits, and sets a product's signals,
on the same flat-registry pattern as [Location Types](/guides/admin/location-types/) and [Tags](/guides/admin/tags/).
Where a [vendor](/guides/admin/vendors/) names who a device comes from, a driver names how it is
talked to (for example `Generic SNMP` or `Cisco xAPI`). Each row shows the **name** (the
operator-facing name, for example `snmp-generic`), the **label**, an optional
**version**, and its **origin** (**official** or **custom**). A driver also carries
an `id`, a uuid minted by the database, the internal address the handle resolves to
([ADR-0062](/architecture/decisions/)); the handle is what you type and read.

A driver is consumed by the [product](/guides/admin/products/) catalog: a `product` references
its driver through an optional `driver_id` to say which driver reads it, chosen from a driver
picker on the product's create and edit forms, and three of the shipped official products bind a
driver this way. It is a leaf catalog beside the vendor registry. See
[core entities](/architecture/core-entities/) for where it sits in the fleet model.

- **New driver** (with `driver:create`, an admin permission) opens a create drawer: give it a
  **name** (unique tenant-wide, e.g. `snmp-generic`), a **label**, and,
  optionally, a **version**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `driver:update`) edits
  the label and version; the **name** is fixed, since a catalog row carries no rename
  (`:rename` is a component, system, location, and principal group affordance). **Delete**
  (with `driver:delete`) removes the row, behind a confirm. A verb you lack greys just that
  button, its hover reason naming the permission (`Requires driver:update`, `Requires driver:delete`);
  the pair never disappears.
- An **official** row is always read-only: the blade keeps the Edit and Delete pair in place,
  greyed, with the reason on hover: "Official: ships with Omniglass and updates with it."
  Omniglass ships a starter set of official drivers (Generic SNMP, Cisco
  xAPI, Crestron CIP, HTTP JSON), upserted idempotently at boot so the shared set cannot drift
  install to install; add a custom driver for anything else.
- **Delete** carries no in-use guard: a [product](/guides/admin/products/) references a `driver`
  through its optional `driver_id`, but that link is `on delete set null`, so deleting a driver
  detaches it from those products (their driver clears) rather than blocking. Removing a custom row
  is unconditional (still refused for an official row, 422). The 409 delete-refused-while-referenced
  rule the [Location Types](/guides/admin/location-types/) registry enforces lives instead on `component.product_id`
  (a product with components cannot be deleted), not on the driver.

Minting a driver is admin-gated; the picker that consumes it lives on the
[product](/guides/admin/products/) create and edit forms. The same operations are `omniglass driver
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
