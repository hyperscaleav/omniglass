---
title: Vendors
description: "The Vendors catalog: the organizations behind products (name, display name, kind of manufacturer/integrator/developer, icon, support phone, website), seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Components > Vendors** (`/vendors`, with `vendor:read`, covered by every viewer's `*:read`
floor) is the directory of **vendors**: the organizations behind the products in the estate, on
the same flat-registry pattern as [Location Types](/guides/admin/location-types/) and [Tags](/guides/admin/tags/).
A vendor is not a device; it is the company a device comes from. Each row shows the **name**
(for example `crestron`), the **display name**, its **kind**
(**manufacturer**, **integrator**, or **developer**), an optional **icon** glyph key, and its
**origin** (**official**, seed-owned, or **custom**). A vendor also carries an `id`, a uuid
minted by the database, the internal address the handle resolves to
([ADR-0062](/architecture/decisions/)); the handle is what you type and read.

A vendor is consumed by the [product](/guides/admin/products/) catalog: a `product` ("Acme 123A,
by Acme") references its vendor through an optional `vendor_id`, chosen from a vendor picker on
the product's create and edit forms, and a component then points at that product. Several shipped
official products carry a vendor this way. See
[core entities](/architecture/core-entities/) for where the vendor registry sits in the estate
model, and [Drivers](/guides/admin/drivers/) and [Capabilities](/guides/admin/capabilities/) for
the two leaf catalogs beside it.

- **Kind** classifies the organization: a **manufacturer** builds hardware, an **integrator**
  assembles and installs systems, a **developer** ships software. It defaults to **manufacturer**
  and is a closed set; a value outside it is refused (422).
- **New vendor** (with `vendor:create`, an admin permission) opens a create drawer: give it a
  **name** (unique tenant-wide, e.g. `crestron`) and a **display name**;
  choose its **kind** (defaults to manufacturer); **icon** (a glyph key), **support phone**, and
  **website** are optional.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `vendor:update`) edits
  the display name, kind, icon, support phone, and website; the **name** is fixed, since a catalog
  row carries no rename. **Delete** (with `vendor:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships eight official vendors (Crestron, Biamp, QSC, Shure,
  Cisco, Extron, Sony, Samsung), all manufacturers, as a starter baseline, upserted idempotently
  at boot so the shared set cannot drift install to install; add a custom vendor for anything else.
- **Website** is validated to an `http`/`https` URL, on both the create/edit form and the API: a
  value in another scheme (for example `javascript:`) is refused with a 422 rather than stored. A
  valid website renders as a live link on the blade; a value that fails the check (entered off
  console, through a raw API call that bypassed the client) still renders, as plain text, never as
  a dead or unsafe link.
- **Delete** carries no in-use guard: a [product](/guides/admin/products/) references a `vendor`
  through its optional `vendor_id`, but that link is `on delete set null`, so deleting a vendor
  detaches it from those products (their vendor clears) rather than blocking. Removing a custom row
  is unconditional (still refused for an official row, 422). The 409 delete-refused-while-referenced
  rule the [Location Types](/guides/admin/location-types/) registry enforces lives instead on `component.product_id`
  (a product with components cannot be deleted), not on the vendor.

Minting a vendor is admin-gated; the picker that consumes it lives on the
[product](/guides/admin/products/) create and edit forms. The same operations are `omniglass vendor
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
