---
title: "Console: the Types catalog page"
date: 2026-07-14
refs: []
form: bullet
---

- **Console: the Types catalog page.** The UI follow-up to the type registry CRUD API above:
  **[Catalog > Types](/guides/admin/types/)** (`/types`) is a **segmented tab control** over all four
  classifier registries (`location_type`, `system_type`, `component_type`, `secret_type`), the first catalog
  page to span several registries in one page rather than a directory per primitive. Each tab (Location,
  System, Component, Secret) renders its own `FlatList` directory scoped to that kind's rows, so the tab is
  the facet: no cross-kind table and no kind filter to type in. Name and official/custom still narrow the
  active tab, and rows are addressed `<kind>:<id>` on the shared blade stack since an id is unique only
  within its own kind. The create drawer opens scoped to the active tab when it is a writable kind (location,
  system, component); an **official** (seed-owned) row and every row on the Secret tab render with neither
  Edit nor Delete, the Secret tab instead showing each row's declared fields (name, scalar type, secrecy,
  origin) read-only, with a note that the fields-schema editor is a follow-up. The page reuses the shell
  built for the identity surfaces (`FlatList`, `BladeStack`, the blade edit contract) rather than a bespoke
  layout, so a fifth classifier registry is a `TYPE_KINDS` entry and a route, not a new page shape. Proven by
  a web data-layer unit suite (`lib/types.ts`): the four-registry aggregation tags each row's kind, a
  location row carries its icon and a secret row its fields, and create/update/delete each refuse the
  `secret` kind without a network call. Deferred: the `secret_type` fields-schema editor (noted above) and a
  page-level component test of the blade CRUD flow.
