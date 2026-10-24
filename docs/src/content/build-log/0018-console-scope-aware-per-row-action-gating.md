---
title: "Console: scope-aware per-row action gating"
date: 2026-07-06
refs: []
form: bullet
---

- **Console: scope-aware per-row action gating.** The read side now annotates every inventory row with the
  scope-aware `actions` it permits (create-a-child / update / delete), computed from the **same** per-action
  `visible_set` the gateway enforces (a new batch `InScopeIDs` primitive answers a whole page in one query per
  action). The console's `ListView` gates each row's Add-child / Edit / Delete affordance on `row.actions`
  rather than the coarse `can(...)` capability, so a scoped operator (e.g. `viewer@all + writer@root`, or a
  `deploy` grant) sees only the buttons the server would actually allow, per row, with no client-side scope
  math. `can(...)` stays the coarse nav/section hint; the server remains the only authority. Proven by a
  storage test (batch membership across all / rooted / exclude-root scopes) and a real-binary API test (owner
  gets every action on every row, a viewer none, a `deploy @ root (exclude_root)` gets create-only on the root
  and create+update on descendants). Thin cut: the list carries `actions`; single-item GET annotation is a
  follow-on (the blades render from the list rows).
