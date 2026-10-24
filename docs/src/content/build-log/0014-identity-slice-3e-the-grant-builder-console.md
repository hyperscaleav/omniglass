---
title: "Identity slice 3e: the grant builder (console)"
date: 2026-07-06
refs: []
form: bullet
---

- **Identity slice 3e: the grant builder (console).** A UI-only refinement of slice 3c: the user card's
  three stacked selects become a **filter-bar-style scope editor**. A keyboard-staged combobox (type a
  role, Tab/Enter to the scope kind, then the entity as an indented tree) commits each grant as a chip,
  and the whole set of changes is staged locally and applied only on **Save** (stage, preview, save), so
  there are no accidental or unclear edits: removing an existing grant marks it (dimmed, undoable) instead
  of firing a live `DELETE`, and a pending-diff preview shows exactly what Save will grant and revoke. No
  new API: it drives the slice-3c `POST` / `DELETE` grant routes, applying adds before removes so an owner
  swap never trips the last-owner guard mid-batch. The staging core (draft, per-chip state, add/remove
  diff, stage validation) is a pure unit-tested module (`lib/grantdraft`); the `GrantBuilder` component
  takes its roles, entity trees, and save mutation as props, proven by a component test that drives the
  keyboard pipeline and asserts the saved diff. Deferred: previewing a grant's *effective access* across
  the scope subtree (#89) and group grants.
