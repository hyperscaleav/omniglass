---
title: Standards
description: "The Standards catalog: the blueprint a system conforms to, its variants, and the properties it declares; shipped standards are yours to edit, not read-only seed content."
---

**Catalog > Standards** (`/standards`, with `standard:read`, covered by every viewer's `*:read` floor)
is the directory of **standards**: the blueprints your systems are built against. A **Huddle Room**, a
**Classroom**, an **Auditorium** are standards; the meeting room on the third floor is a
[system](/guides/operator/inventory/) that **conforms** to one. Each row shows the **id**, the
**display name**, its **parent standard** if it is a variant, and its **origin**.

A standard is the **system-side counterpart of a [product](/guides/admin/products/)**. A product says
what a component *is* and what it carries; a standard says what a system is built to be and what it
carries. This is the promotion of the old `system_type` registry, which was only a label: a standard
declares a contract, so it earned its own catalog page and its own `standard:*` permission rather than
sitting under the shared type registry.

- **Conforming is optional.** A system points at a standard through its **Standard** field, and
  leaving it empty is legitimate: a **one-off system** that matches no blueprint carries only its own
  values, exactly as a component with no product does.
- **Variants use parent standard.** A specific blueprint that refines a broader one points at it with
  **parent standard** (a Large Classroom under Classroom). A standard with no parent is a base
  standard.
- **New standard** (with `standard:create`, an admin permission) opens a create drawer: name its **id**
  (a kebab identifier, unique tenant-wide, e.g. `lecture-hall`), give it a **display name**, and
  optionally pick a **parent standard**. An unknown parent is refused (422).
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `standard:update`) edits the
  display name and parent; the id is fixed. **Delete** (with `standard:delete`) removes the row, behind
  a confirm.
- **Delete is refused (409) while a system still conforms to it.** Repoint or remove those systems
  first.

The same operations are `omniglass standard list/get/create/update/delete` from the CLI (see the
[CLI reference](/reference/cli/)).

## The shipped standards are yours

Omniglass ships a starter set (Meeting Room, Huddle Room, Classroom, Auditorium, Digital Signage,
Control Room), and unlike a seeded [vendor](/guides/admin/vendors/) or
[product](/guides/admin/products/) those rows are **not official and not read-only**. They arrive as
ordinary **operator-owned** rows: rename them, re-parent them, add to their contract, delete the ones
you do not run.

This is deliberate. A standard is **forked from a template that lives in the code**, once, with **no
inheritance**: nothing in your estate points back at that template afterwards. So Omniglass can improve
its templates in any release without ever touching your rows, and your edits are never reverted by a
restart, because the boot seed installs a standard **only if it is absent**. The thing the vendor
updates and the thing you own are two different objects, which is the whole point.

The exception is the **canonical vocabulary**: the [property catalog](/guides/admin/properties/) (and
later commands and event types) is the shared namespace a driver maps onto, so it must read the same on
every install. Those entries stay **official** and read-only, and a release can correct them. See
[the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)
for the full reasoning.

## Declared properties: the standard's contract

A standard's blade carries a **Declared properties** panel, its **contract**: which
[properties](/guides/admin/properties/) every system conforming to it exposes, and what each one
defaults to. It is the same editor as a product's contract, on the system side.

- **Declare a property** (with `standard:update`) picks a name from the property catalog, optionally
  types a **default**, and optionally marks it **required**. The property must already exist in the
  catalog, since the contract only names it: mint it under
  [Catalog > Properties](/guides/admin/properties/) first. Declaring is **idempotent**, so declaring a
  property already on the contract revises that line in place.
- **The default is typed by the catalog, not here.** Type and validation live on the property, so a
  standard cannot redefine what `room_capacity` means, only what a system conforming to it starts with.
- **Conformance is live, not a copy.** A system does **not** fork its standard. Change a contract
  default and every conforming system that has not overridden that property picks up the new value
  immediately. Only a system's own override survives the change.
- **Required** means a conforming system must resolve the property to a value; the system's Properties
  panel blocks Save while a required property is empty.
- **Withdraw** (with `standard:delete`, behind a confirm) removes a line. Conforming systems **keep**
  any value they set for it; it simply reads as **off contract** from then on.

From the CLI the contract is `omniglass standard properties <id>`,
`omniglass standard set-property <id> <property>`, and
`omniglass standard delete-property <id> <property>`.
