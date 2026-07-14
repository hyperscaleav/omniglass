---
title: Types
description: "The Types catalog: a segmented tab per kind across the location, system, component, and secret type registries, each tab its own directory, with CRUD for the three writable kinds."
---

**Catalog > Types** (with `type:read`, covered by every viewer's `*:read` floor) is a
**segmented tab control** over the four classifier registries that shape the estate:
**Location**, **System**, **Component**, and **Secret**, one tab per `location_type`,
`system_type`, `component_type`, and `secret_type`. Each tab is that registry's own directory:
a table of its rows, sorted alphabetically by display name, each showing **id**, **display name**, and **origin** (**official**, seed-owned, or **custom**); the Location tab's rows
also carry an **icon** glyph key.

- Switch tabs to move between registries; **name** matches an id or display name within the
  active tab, and **official** narrows it to official or custom rows.
- **New type** (with `type:create`, an admin permission) opens a create **drawer** scoped to
  the active tab when it is a writable kind (location, system, or component; the Secret tab has
  no write routes this slice): name its **id** (a kebab identifier, unique within that kind,
  e.g. `wing`), give it a **display name**, and, on the
  Location tab, an **icon** glyph key (defaults to `map-pin`) and its **allowed parents**: a
  checkbox list of the other location types plus a **Root** option, the set of parent types (or
  the top of the tree) a location of this type may be placed under. Leave every box unchecked for
  **unconstrained** (any parent, or root), the default; `root` is a reserved id no real type may
  take.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `type:update`) edits
  the display name and, on a location type, the icon and allowed parents; the kind and id are
  fixed. **Delete** (with `type:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks
  it "Seed-owned, read-only." It is part of the baseline every estate ships with (for example
  `campus` / `building` / `floor` / `room` for locations), so the shared vocabulary cannot drift
  from install to install.
- The **Secret** tab is read-only too: each row shows the type's declared **fields** (name,
  scalar type, whether the field is itself secret, and its origin), so you can see what a secret
  of that type expects (for example `snmp-community` or `basic-auth`), but there is no write
  route yet. Editing the fields schema is a follow-up.
- **Delete** is refused (409) while a location, system, or component still uses that type:
  reclassify or remove the referencing rows first. An official type cannot be deleted at all
  (422).
- A non-empty **allowed parents** set is enforced when a location is created or moved: an
  out-of-order placement (for example a floor under a room) is refused with a message naming
  both types. An empty set never blocks a placement. Existing locations are grandfathered: adding
  or tightening a set does not touch a placement already made, only a later move.

Minting a type is admin-gated; *using* one, classifying a location, system, or component by
picking it on that entity's own create or edit form, is the ordinary entity write, gated by that
entity's own `:update`. The same operations are `omniglass type <kind> list/create/update/delete`
from the CLI (see the [CLI reference](/reference/cli/)).
