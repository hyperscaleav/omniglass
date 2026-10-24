---
title: "create is a route; inventory create and edit unify on the detail accordion"
date: 2026-07-14
status: "Accepted"
summary: "Inventory create/edit unify on the detail accordion: `New` routes to `/<entity>/create` (a draft) and Save hands off to `/<entity>/<id>` in edit; view is read-only, edit is the sole writer; the create/edit Drawer is retired"
---

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/), [core entities](/architecture/core-entities/)
- **Decision:** The inventory entities (component, system, location) drop the create/edit **Drawer**. Creating one
  is now a **route**: `New` navigates to `/<entity>/create`, a **draft accordion** where Identity and Placement are
  writable and the binding sections (Tags, and later Secrets/Variables) are shown locked until the entity exists;
  **Save** commits the row and hands off to `/<entity>/<id>` in **edit mode** (a one-shot pending-edit flag consumed
  when the detail resolves, the Users `openPrincipalInEdit` pattern). The detail is one accordion, **read-only in
  view and the sole writer in edit**: no in-body field or binding mutation control renders while not editing (the
  footer's Edit / Delete chrome and the read-only effective-secrets/variables panels are exempt). This is the Users
  inline-blade-edit model generalised to inventory, and it holds on **both** the docked blade and the addressable
  full page. No new routes: the static `/create` outranks `/:name` in the router, so `create` is a reserved segment.
  The shared `TreeList` primitive gains a per-surface **edit slot on `ListCtx`** (the full page makes its own slot,
  since the shared `renderDetail` must not call `useBladeEdit` outside a blade provider), plus `renderCreate` /
  `onNew` / `onEdit` hooks and an optional `FormBody`, so a page opts into the model without breaking the others.
- **Context:** Creating an inventory entity returned you to the list, so setting a tag meant find, reopen, edit; and
  `TagAdder` rendered a write control in view. A drawer that opened in edit after create would need a fragile
  cross-surface hand-off (the code-grounded review of the drawer design surfaced a full-page `useBladeEdit` crash, a
  `FormBody` footer collision, and a pending-edit gap). Framing create as its own URL dissolved the "create is
  blade-only" false dilemma: a draft with an address is deep-linkable full-page and dockable as a blade, and Save is
  a route hand-off, not a surface hop. Own-field edits commit on Save (Cancel reverts them); tag bindings keep their
  immediate per-binding write, so Cancel does not roll a tag back, and the tag control sits apart from the Save/Cancel
  form. Slice 2 (a shared cross-page form shell) and slice 3 (moving Users onto it) are deferred.
- **Closes:** issue [#231](https://github.com/hyperscaleav/omniglass/issues/231).
