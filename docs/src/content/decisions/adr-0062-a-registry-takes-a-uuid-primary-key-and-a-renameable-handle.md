---
title: "A registry takes a uuid primary key and a renameable handle"
date: 2026-07-22
status: "Accepted"
summary: "A registry takes a uuid primary key and a renameable handle"
---

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [api-first](/contributing/api-first/)
- **Decision:** a registry has a **uuid `id`** and a **unique, renameable `name`**, the shape `tag` and every
  estate entity already have. `product` and `vendor` convert first; the remaining seven follow, slice by
  slice, tracked as [#262](https://github.com/hyperscaleav/omniglass/issues/262).
- **Context:** [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key) says every foreign key stores
  its target's primary key, and epic #343 made that true everywhere **except** the slug-keyed registries,
  where the name *is* the key. That exception was the last place a foreign key referenced a mutable,
  human-authored string. A product id was a typo or a rebrand away from being wrong forever, and two
  device packs both defining `cisco-room-kit-pro` collide on the primary key itself.
- **`name`, not `slug` or `key`.** Six tables and every estate entity already call the human handle `name`,
  and the API bodies already say `name`. A third word for the same concept would be worse than the
  inconsistency it fixed. Renaming the family to `slug` later is a separate, mechanical decision.
- **The registries already disagreed with each other**, which is worth recording: `property` and
  `interface_type` call their slug `name`, while `capability`, `driver`, `location_type`, `secret_type`,
  and `standard` call theirs `id`. So the later slices are a **rename** for five of them and an addition
  for two, not one uniform change.
- **`node` stays the exception.** Its primary key is `principal_id`, because a node is the detail row of a
  principal and its key IS that foreign key. It is deliberate and it is not changing.
- **The API carries both and accepts either**, as the estate entities do: `id` (uuid) and `name` (handle)
  on every body, and a path or reference resolves whichever form it is given. A name can never
  look like a uuid, so the two cannot collide.
- **The rename test is written first, each slice.** It renames a handle and asserts every reference still
  resolves and now reads the new one. That is the capability the epic buys, so it is what the slice proves.
- **The exception is retired, not just the tables.** With all nine registries converted, a closing slice
  removes the slug-keyed carve-out from the doctrine: the [api-first](/contributing/api-first/) rule now
  states every foreign key stores a uuid with no exception, [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key)'s
  carve-out is marked retired, and the `TestReferencesCarryBothForms` guard drops its slug-keyed
  allow-list (the registry references move into the both-forms rule, and the one that surfaced a real
  gap, a component response that carried `product_id` without the product's name, is fixed). The
  storage helper collapses too: the per-registry `productRefCol` / `vendorRefCol` and the
  `registryHandles` set fold into one `registryRefCol(ref)`, since every registry now behaves the same.
- **Extended by [ADR-0089](#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup):**
  this decision's dual-accept clause already said a reference "resolves whichever form it is given";
  ADR-0089 makes a third form, a dotted path, a real one for `location`, `system`, and `component`,
  resolved structurally to a uuid before the ordinary name-or-id lookup runs.
