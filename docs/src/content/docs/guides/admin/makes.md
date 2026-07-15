---
title: Makes
description: "The Makes registry: the manufacturer catalog (id, display name, icon, support phone, website) behind the component model product catalog, seed-owned official rows read-only, admin-gated custom ones."
---

**Catalog > Makes** (`/component-makes`, with `make:read`, covered by every viewer's `*:read`
floor) is the directory of **component makes**: the manufacturers a component can be made by, on
the same flat-registry pattern as [Types](/guides/admin/types/) and [Tags](/guides/admin/tags/).
Each row shows the **id**, the **display name**, an optional **icon** glyph key, and its **origin**
(**official**, seed-owned, or **custom**).

It is the first landed piece of a larger make/model catalog: every [component model](/guides/admin/models/)
("Acme 123A, made by Acme") references a make, but nothing on a `component` instance points at
either one yet. A `component_type` genus tree and model-to-component assignment are later slices of
the same catalog effort, not built yet. See [core entities](/architecture/core-entities/) for where
the make registry sits in the estate model.

- **New make** (with `make:create`, an admin permission) opens a create drawer: name its **id** (a
  short identifier, unique tenant-wide, e.g. `crestron`) and give it a **display name**; **icon**
  (a glyph key), **support phone**, and **website** are optional.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `make:update`) edits
  the display name, icon, support phone, and website; the id is fixed. **Delete** (with
  `make:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships eight official makes (Crestron, Biamp, QSC, Shure,
  Cisco, Extron, Sony, Samsung) as a starter vendor baseline, upserted idempotently at boot so the
  shared set cannot drift install to install; add a custom make for anything else.
- **Website** is validated to an `http`/`https` URL, on both the create/edit form and the API: a
  value in another scheme (for example `javascript:`) is refused with a 422 rather than stored. A
  valid website renders as a live link on the blade; a value that fails the check (entered off
  console, through a raw API call that bypassed the client) still renders, as plain text, never as
  a dead or unsafe link.
- **Delete** is refused (409) while a [component model](/guides/admin/models/) still references the
  make, the same delete-refused-while-referenced rule the [Types](/guides/admin/types/) registry
  already enforces; a make nothing points at deletes unconditionally (still refused for an official
  row, 422).

Minting a make is admin-gated. The picker that consumes it, choosing a model's make on the
[Models](/guides/admin/models/) page, ships alongside `component_model`. The same operations are
`omniglass component-make list/get/create/update/delete` from the CLI (see the
[CLI reference](/reference/cli/)).
