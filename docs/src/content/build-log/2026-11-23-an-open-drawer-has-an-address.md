---
title: "An open drawer has an address on every list page (#2429)"
date: 2026-11-23
refs: [2429, 2467]
---

Opening a row's drawer on a list page left the URL unchanged, so a rule, driver, product or user could
not be linked, a reload lost the place, and Back left the list instead of closing the drawer.

The list primitive now holds the open row in `?open=<row address>` (the address its API route takes,
ADR-0062), through one hook, `useDrawerAddress` in `lib/drawer_address.ts`, mounted by `FlatList` and
`TreeList`. Opening pushes history (Back closes), closing clears the param in place, and loading an
address opens the drawer. An address naming no visible row shows the list with a not-found notice in
`ListShell`, the same words for a missing and an out-of-scope row. A nested blade keeps its own stack
and stays out of the URL; the param composes with a page's filter, sort and `?edit=1`. The older
`?u=`, `?g=` and `?n=` ids still open, and the Users, Groups and Outposts pages now write `?open=`.

`drawerHref(kind, address)` is the one way to link into a drawer from another surface; the command
palette's entity results use it (`Command.entity`). The route manifest's smoke tier visits one
`?open=` address. Tests: `DrawerAddress.test.tsx` (flat drawer, flat blade, tree) and
`drawer_address.test.ts`.
