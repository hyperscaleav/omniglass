---
title: Types
description: "The Types catalog: a segmented tab per kind across the location and secret type registries, each tab its own directory, with CRUD and a property contract on the writable kind."
---

**Catalog > Types** (with `type:read`, covered by every viewer's `*:read` floor) is a
**segmented tab control** over the classifier registries that shape the estate:
**Location** and **Secret**, one tab per `location_type` and `secret_type`. Two kinds have
**left** this page for catalogs of their own, because a bare label registry was never what they
were: a **component**'s shape comes from the [product](/guides/admin/products/) it points at, and a
**system**'s from the [standard](/guides/admin/standards/) it conforms to. Each remaining tab is that
registry's own directory: a table of its rows, sorted alphabetically by display name, each showing
**id**, **display name**, and **origin** (**official**, seed-owned, or **custom**); the Location tab's
rows also carry an **icon** glyph key.

- Switch tabs to move between registries; **name** matches an id or display name within the
  active tab, and **official** narrows it to official or custom rows.
- **New type** (with `type:create`, an admin permission) opens a create **drawer** scoped to
  the active tab when it is a writable kind (location; the Secret tab has no write routes this
  slice): name its **id** (a kebab identifier, unique within that kind, e.g. `wing`), give it a
  **display name**, an **icon** glyph key (defaults to `map-pin`), and its **allowed parents**: a
  checkbox list of the other location types plus a **Root** option, the set of parent types (or
  the top of the tree) a location of this type may be placed under. Leave every box unchecked for
  **unconstrained** (any parent, or root), the default; `root` is a reserved id no real type may
  take.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `type:update`) edits
  the display name, the icon, and the allowed parents; the kind and id are fixed. **Delete** (with
  `type:delete`) removes the row, behind a confirm.
- **The four shipped location types are yours to change.** `campus`, `building`, `floor`, and `room`
  arrive as ordinary **operator-owned** rows (origin **custom**, not official), so rename them,
  re-parent them, or delete the ones your estate does not use. They are forked from a template in the
  code, once, with no inheritance, and the boot seed installs them **only if absent**, so a restart
  never reverts your edits. See
  [the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs).
- An **official** (seed-owned) row, where one exists, is read-only: no Edit, no Delete, and the blade
  marks it "Seed-owned, read-only." That treatment is reserved for the **canonical vocabulary** a
  release must be able to correct, above all the [property catalog](/guides/admin/properties/), not for
  example content like a place type.
- The **Secret** tab is read-only too: each row shows the type's declared **fields** (name,
  scalar type, whether the field is itself secret, and its origin), so you can see what a secret
  of that type expects (for example `snmp-community` or `basic-auth`), but there is no write
  route yet. Editing the fields schema is a follow-up.
- **Delete** is refused (409) while a location still uses that type: reclassify or remove the
  referencing rows first.
- A non-empty **allowed parents** set is enforced when a location is created or moved: an
  out-of-order placement (for example a floor under a room) is refused with a message naming
  both types. An empty set never blocks a placement. Existing locations are grandfathered: adding
  or tightening a set does not touch a placement already made, only a later move.

Minting a type is admin-gated; *using* one, classifying a location by picking it on the location's own
create or edit form, is the ordinary entity write, gated by `location:update`. The same operations are
`omniglass type location list/create/update/delete` from the CLI (see the
[CLI reference](/reference/cli/)).

## Declared properties: the location type's contract

A location type's blade carries a **Declared properties** panel, its **contract**: which
[properties](/guides/admin/properties/) every location of that type exposes, and what each one defaults
to. It is the same editor as a [product](/guides/admin/products/)'s or a
[standard](/guides/admin/standards/)'s, and it keeps this page's permission story: the read rides
`type:read`, declaring a line needs `type:update`, and withdrawing one needs `type:delete`.

- **Declare a property** picks a name from the property catalog, optionally types a **default**, and
  optionally marks it **required**. The property must already exist in the catalog; mint it under
  [Catalog > Properties](/guides/admin/properties/) first. Declaring is **idempotent**.
- **Type and validation are the catalog's**, not the contract's, so a location type cannot redefine
  what a property means, only what a fresh location of that type starts with.
- **Locations inherit live.** Change a default and every location of the type that has not overridden
  that property picks up the new value. **Withdraw** removes the line; locations keep any value they
  set, now **off contract**.

From the CLI the contract is `omniglass location-type property list <id>`,
`omniglass location-type property update <id> <property>`, and
`omniglass location-type property delete <id> <property>`. Note the command name: the registry CRUD is
`omniglass type location ...`, while the contract hangs off `omniglass location-type ...`, mirroring the
routes (`/types/location` for the registry, `/location-types/{id}/properties` for its contract).
