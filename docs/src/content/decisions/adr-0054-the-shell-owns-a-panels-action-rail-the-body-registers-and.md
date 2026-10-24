---
title: "The shell owns a panel's action rail; the body registers and never draws"
date: 2026-07-21
status: "Accepted"
summary: "A panel's action bar is **declared, not laid out**: a blade body binds through `lib/blades`, a Drawer form body through `lib/formactions`, and `BladeStack` / `Drawer` draw the result through the one `PanelFooter` rail. The opt-in `DrawerFooter` helper is deleted. A convention a body must remember can be forgotten, and was, by two forms for months while it was copied into six new pages around them"
---

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/)
- **Decision:** A panel's action buttons are **declared as data, not laid out as markup**. A blade body
  binds `destructive` / `secondary` / `primary` (plus the Edit -> Save cycle) through `lib/blades`; a
  Drawer form body binds `submitLabel` / `submitIcon` / `submit` / `busy` / `disabled` / `cancel`
  through `lib/formactions`. `BladeStack` and `Drawer` each compose their own button vocabulary but
  draw it through the single `PanelFooter` rail. A form body renders **no footer markup at all**.
- **Context:** The blade already worked this way. The Drawer did not: its rail was an opt-in
  `DrawerFooter` helper that every form body had to remember to import and wrap its buttons in.
  Fourteen forms remembered. Two did not, and hand-rolled their own right-aligned row instead, where
  they survived nine merged PRs unchanged while the helper was copied into six newly added pages
  around them. The cost was not only the two misses: among the forms that did use it, some rendered a
  Cancel and some did not, some spun a spinner and some swapped the label to "Creating...". A rail
  reached by convention drifts in both directions at once.
- **Why a slot rather than a lint rule:** A lint rule finds the miss after it is written. A slot makes
  it unwriteable: there is no exported helper to forget, and a body that wants a button has exactly
  one way to ask for one. The enforcement is the deleted export, and `rail-ownership.test.ts` is the
  belt to that braces.
- **Scope, honestly:** This converges **two** of the three rails. Full-page create forms
  (Locations, Systems, Components) still draw an inline `border-t ... pt-4` row of their own. That
  rail is inline in a scrolling page rather than pinned to the viewport, so it is a different layout
  problem, and it converges when the CRUD form primitive lands and owns both form factors.
- **Deliberate convergences:** submit labels no longer change while in flight (the shell's spinner
  says it), and the new-interface blade lost its Cancel button, since a blade already dismisses two
  ways and no other blade in the stack carries one.
- **Tracked under** [#332](https://github.com/hyperscaleav/omniglass/issues/332).
