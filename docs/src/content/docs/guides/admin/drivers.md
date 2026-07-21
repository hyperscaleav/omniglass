---
title: Drivers
description: "The Drivers catalog: the implementations that get, emit, and set a product's signals (id, display name, version), seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Drivers** (`/drivers`, with `driver:read`, covered by every viewer's `*:read` floor)
is the directory of **drivers**: the implementation that gets, emits, and sets a product's signals,
on the same flat-registry pattern as [Types](/guides/admin/types/) and [Tags](/guides/admin/tags/).
Where a [vendor](/guides/admin/vendors/) names who a device comes from, a driver names how it is
talked to (for example `Generic SNMP` or `Cisco xAPI`). Each row shows the **id**, the **display
name**, an optional **version**, and its **origin** (**official**, seed-owned, or **custom**).

Today a driver stands alone: nothing in the estate points at one yet. It is a leaf catalog beside
the vendor and [capability](/guides/admin/capabilities/) registries, the layer a future `product`
will reference to say which driver reads it. See [core entities](/architecture/core-entities/) for
where it sits in the estate model.

- **New driver** (with `driver:create`, an admin permission) opens a create drawer: name its **id**
  (a short identifier, unique tenant-wide, e.g. `snmp-generic`), give it a **display name**, and,
  optionally, a **version**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `driver:update`) edits
  the display name and version; the id is fixed. **Delete** (with `driver:delete`) removes the row,
  behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships a starter set of official drivers (Generic SNMP, Cisco
  xAPI, Crestron CIP, HTTP JSON), upserted idempotently at boot so the shared set cannot drift
  install to install; add a custom driver for anything else.
- **Delete** carries no in-use guard in this slice: nothing yet references a `driver`, so removing a
  custom row is unconditional (still refused for an official row, 422). A later slice that lands
  `product` adds the referential guard (409 while a product still points at the driver), the same
  delete-refused-while-referenced rule the [Types](/guides/admin/types/) registry already enforces.

Minting a driver is admin-gated; the picker that consumes it, choosing a product's driver, does not
exist yet, since it waits on `product`. The same operations are `omniglass driver
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
