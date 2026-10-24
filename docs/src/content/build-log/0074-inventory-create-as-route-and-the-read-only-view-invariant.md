---
title: "Inventory: create-as-route and the read-only view invariant"
date: 2026-07-14
refs: []
form: bullet
---

- **Inventory: create-as-route and the read-only view invariant.** Creating a component, system, or location no
  longer returns you to the list. `New` navigates to `/<entity>/create`, a **draft accordion** (Identity and
  Placement writable, the Tags section locked until the entity exists); **Save** commits the row and hands off to
  `/<entity>/<id>` in **edit mode** via a one-shot pending-edit flag, so you tag and finish configuring in place.
  The detail is one accordion, **read-only in view and the sole writer in edit**: the own-field inputs and the
  `TagAdder` write controls appear only in edit (the footer Edit/Delete and the read-only effective-secrets/variables
  panels, both since retired ([#281](https://github.com/hyperscaleav/omniglass/issues/281)), were exempt), which
  retires the old drawer-reopen edit and the mutation-in-view. This is the Users
  inline-blade-edit model generalised to inventory, on both the docked blade and the full page
  ([ADR-0027](/architecture/decisions/#adr-0027-create-is-a-route-inventory-create-and-edit-unify-on-the-detail-accordion),
  [#231](https://github.com/hyperscaleav/omniglass/issues/231)). The shared `TreeList` gains a per-surface edit slot
  on `ListCtx` (the full page makes its own, since the shared `renderDetail` must not call `useBladeEdit` outside a
  blade provider), plus `renderCreate` / `onNew` / `onEdit` hooks and an optional `FormBody`. Proven by the live
  console (draft to create to edit hand-off, then a read-only fresh visit) and per-page web tests for the three
  entities (the draft route renders, the view has no tag-add control). Deferred: a shared cross-page form shell
  (slice 2) and moving the Users page onto it (slice 3).
