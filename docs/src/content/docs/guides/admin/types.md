---
title: Types
description: "The Types catalog: one directory across the location, system, component, and secret type registries, with a kind facet and CRUD for the three writable kinds."
---

**Catalog > Types** (with `type:read`, covered by every viewer's `*:read` floor) is the unified
directory across the four classifier registries that shape the estate: `location_type`,
`system_type`, `component_type`, and `secret_type`. One table lists all four; each row shows the
**kind**, **id**, **display name**, **rank** (its sort order within the kind), and **origin**
(**official**, seed-owned, or **custom**); a location row also carries an **icon** glyph key.

- Type a **kind** into the filter bar to narrow the table to one registry at a time (location,
  system, component, or secret); **name** matches an id or display name, and **official** narrows
  to official or custom rows.
- **New type** (with `type:create`, an admin permission) opens a create **drawer**: pick a
  writable **kind** (location, system, or component; secret types have no write routes this
  slice), name its **id** (a kebab identifier, unique within that kind, e.g. `wing`), give it a
  **display name** and a **rank** (lower sorts first), and, for a location type, an **icon**
  glyph key (defaults to `map-pin`).
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `type:update`) edits
  the display name, rank, and, on a location type, the icon; the kind and id are fixed.
  **Delete** (with `type:delete`) removes the row, behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks
  it "Seed-owned, read-only." It is part of the baseline every estate ships with (for example
  `campus` / `building` / `floor` / `room` for locations), so the shared vocabulary cannot drift
  from install to install.
- A **secret** row is read-only here too: it shows the type's declared **fields** (name, scalar
  type, whether the field is itself secret, and its origin), so you can see what a secret of that
  type expects (for example `snmp-community` or `basic-auth`), but there is no write route yet.
  Editing the fields schema is a follow-up.
- **Delete** is refused (409) while a location, system, or component still uses that type:
  reclassify or remove the referencing rows first. An official type cannot be deleted at all
  (422).

Minting a type is admin-gated; *using* one, classifying a location, system, or component by
picking it on that entity's own create or edit form, is the ordinary entity write, gated by that
entity's own `:update`. The same operations are `omniglass type <kind> list/create/update/delete`
from the CLI (see the [CLI reference](/reference/cli/)).
