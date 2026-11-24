---
title: "Interactive primitives are in-house wrappers over Kobalte"
date: 2026-11-23
status: Accepted
summary: "The console leans into Kobalte for dialogs, popovers, tooltips, selects, pickers, tabs, toasts, collapsibles, switches and segmented controls, but only through wrappers in web/src/components/ui/; a guard test fails any other import of @kobalte/core. Native select, checkbox, radio and details stay where they fit; the tree stays ours."
refs: [2485, 2290, 2488]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [Design system](/contributing/design-system/)
- **Decision:** Every interactive primitive the console uses comes from `web/src/components/ui/`
  (`ModalDialog`, `ConfirmDialog`, `Popover`, `Tooltip`, `Select`, `Picker`, `Tabs`, `Toast`,
  `Collapsible`, `Switch`, `SegmentedControl`), each a house-styled component over Kobalte with a
  keyboard-contract test. Only that directory imports `@kobalte/core`, enforced by
  `web/src/kobalte-guard.test.ts`. A surface that needs a primitive with no wrapper adds the
  wrapper first. Native `<select>`, checkbox, radio and `<details>` are not replaced where they
  fit, and the outline tree and `TreeSelect`'s tree stay ours (Kobalte has no tree).
- **Context:** The console's review (2026-11-23) found Kobalte imported directly in 13 files and
  the same patterns hand-rolled elsewhere (tabs, three comboboxes, a modal, 38 `window.confirm`
  calls). The Solid 2 analysis on #2290 raised replacing Kobalte with native primitives; the
  architect ruled the other way: lean into Kobalte, behind our own API, so builders reach for one
  component per pattern, every surface gets the same keyboard and focus behavior, and the library
  under it stays swappable in about ten files (the Solid 2 migration waits for Kobalte 2). The
  wrappers' props are the contract the conversion epic (#2488) consumes. Building them found a
  live defect: Kobalte 0.13 assembles its `Dialog` and `AlertDialog` namespaces by
  `Object.assign` onto one shared root, so once the alert-dialog module loaded, `Dialog.Content`
  was the alert content and every Drawer announced itself as an alertdialog. The wrappers import
  Kobalte's named exports, never those namespaces.
