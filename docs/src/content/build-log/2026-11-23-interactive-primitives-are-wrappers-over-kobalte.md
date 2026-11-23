---
title: "Interactive primitives are in-house wrappers over Kobalte (#2485)"
date: 2026-11-23
refs: [2485, 2488]
---

The console's interactive primitives now live in `web/src/components/ui/`, and a guard test
(`web/src/kobalte-guard.test.ts`) fails any `@kobalte/core` import outside it
([ADR-2485](/architecture/decisions/)). The 13 files that imported Kobalte directly go through
`ModalDialog`, `ConfirmDialog`, `Popover`, `Tooltip` and `Select` with no visual change. Six new
wrappers ship, each with a keyboard-contract test and one consumer: `Picker` (a filterable single
and multi select with a create row, on the simulated component's Product, inside its drawer),
`Tabs` (a role's All / Held / Missing permissions), `Toast` (the toast primitive, now Kobalte's
region: Alt+T, Esc, pause on hover; on a setting's save), `Collapsible` (Profile's permission
list, closed by default), `Switch` (a tag's binding) and `SegmentedControl` (From scratch / From a
template on the create pages).

Fixed on the way: every `Drawer` was announced as an alertdialog, because Kobalte 0.13 assigns
`Dialog`'s and `AlertDialog`'s parts onto one shared root and the alert content won once
`ConfirmDialog` was in the bundle. `ModalDialog` takes the named exports.
