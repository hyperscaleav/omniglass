---
title: Capabilities
description: "The Capabilities catalog: the flat vocabulary of what a component can do (id, display name), like microphone or display, seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Capabilities** (`/capabilities`, with `capability:read`, covered by every viewer's
`*:read` floor) is the directory of **capabilities**: the flat vocabulary of what a component can
do, on the same flat-registry pattern as [Types](/guides/admin/types/) and [Tags](/guides/admin/tags/).
A capability is a plain name of a function (a microphone, a display, a camera), not a device and
not a measurement. Each row shows the **id**, the **display name**, and its **origin**
(**official**, seed-owned, or **custom**).

Today a capability stands alone: nothing in the estate points at one yet. It is a leaf catalog
beside the [vendor](/guides/admin/vendors/) and [driver](/guides/admin/drivers/) registries, the
layer a future `product` will reference to declare what it can do. See
[core entities](/architecture/core-entities/) for where it sits in the estate model.

- **New capability** (with `capability:create`, an admin permission) opens a create drawer: name
  its **id** (a short identifier, unique tenant-wide, e.g. `microphone`) and give it a **display
  name**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `capability:update`)
  edits the display name; the id is fixed. **Delete** (with `capability:delete`) removes the row,
  behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships a starter set of official capabilities (Microphone,
  Speaker, Display, Flat Panel Display, Camera, Codec, Touch Panel), upserted idempotently at boot
  so the shared set cannot drift install to install; add a custom capability for anything else.
- **Delete** carries no in-use guard: a [product](/guides/admin/products/) declares a `capability`
  through the `product_capability` join, but that link is `on delete cascade`, so deleting a
  capability drops it from those products' capability sets rather than blocking. Removing a custom
  row is unconditional (still refused for an official row, 422). The 409
  delete-refused-while-referenced rule the [Types](/guides/admin/types/) registry enforces lives
  instead on `component.product_id` (a product with components cannot be deleted), not on the
  capability.

Minting a capability is admin-gated; the picker that consumes it, declaring a product's
capabilities, does not exist yet, since it waits on `product`. The same operations are `omniglass
capability list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
