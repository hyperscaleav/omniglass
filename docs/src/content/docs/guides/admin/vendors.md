---
title: Vendors
description: "The Vendors catalog: the organizations behind products (id, display name, kind of manufacturer/integrator/developer, icon, support phone, website), seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Vendors** (`/vendors`, with `vendor:read`, covered by every viewer's `*:read`
floor) is the directory of **vendors**: the organizations behind the products in the estate, on
the same flat-registry pattern as [Types](/guides/admin/types/) and [Tags](/guides/admin/tags/).
A vendor is not a device; it is the company a device comes from. Each row shows the **id**, the
**display name**, its **kind** (**manufacturer**, **integrator**, or **developer**), an optional
**icon** glyph key, and its **origin** (**official**, seed-owned, or **custom**).

Today a vendor stands alone: nothing in the estate points at one yet. It is a landed piece of a
larger catalog, the layer a future `product` ("Acme 123A, by Acme") will reference; a `product`
and its assignment to a component are later slices of the same effort, not built yet. See
[core entities](/architecture/core-entities/) for where the vendor registry sits in the estate
model, and [Drivers](/guides/admin/drivers/) and [Capabilities](/guides/admin/capabilities/) for
the two leaf catalogs beside it.

- **Kind** classifies the organization: a **manufacturer** builds hardware, an **integrator**
  assembles and installs systems, a **developer** ships software. It defaults to **manufacturer**
  and is a closed set; a value outside it is refused (422).
- **New vendor** (with `vendor:create`, an admin permission) opens a create drawer: name its
  **id** (a short identifier, unique tenant-wide, e.g. `crestron`) and give it a **display name**;
  choose its **kind** (defaults to manufacturer); **icon** (a glyph key), **support phone**, and
  **website** are optional.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `vendor:update`) edits
  the display name, kind, icon, support phone, and website; the id is fixed. **Delete** (with
  `vendor:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships eight official vendors (Crestron, Biamp, QSC, Shure,
  Cisco, Extron, Sony, Samsung), all manufacturers, as a starter baseline, upserted idempotently
  at boot so the shared set cannot drift install to install; add a custom vendor for anything else.
- **Website** is validated to an `http`/`https` URL, on both the create/edit form and the API: a
  value in another scheme (for example `javascript:`) is refused with a 422 rather than stored. A
  valid website renders as a live link on the blade; a value that fails the check (entered off
  console, through a raw API call that bypassed the client) still renders, as plain text, never as
  a dead or unsafe link.
- **Delete** carries no in-use guard in this slice: nothing yet references a `vendor`, so removing
  a custom row is unconditional (still refused for an official row, 422). A later slice that lands
  `product` adds the referential guard (409 while a product still points at the vendor), the same
  delete-refused-while-referenced rule the [Types](/guides/admin/types/) registry already enforces.

Minting a vendor is admin-gated; the picker that consumes it, choosing a product's vendor, does not
exist yet, since it waits on `product`. The same operations are `omniglass vendor
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
