---
title: "Identity: the Groups console + inherited-vs-direct grants, UI"
date: 2026-07-08
refs: []
form: bullet
---

- **Identity: the Groups console + inherited-vs-direct grants, UI.** A **Groups** page (`Settings > Groups`,
  gated `principal_group:read`) as a config over the shared `FlatList`: list and create, and a row Drawer with
  member add/remove and the grant builder (reused, wired to the group grant endpoints, so a group's members
  inherit what it grants). On the **user** detail, grants now split into **direct** (editable) and **inherited
  from a group** (read-only, tagged `from <group>`), so it is clear where a user's access comes from; the
  principal grant body gained `group_id` / `group_name` for the distinction. Proven by the web suite and tsc,
  with the route guard and sidebar both gating `/groups` on `principal_group`. This completes grant-by-group
  ([#90](https://github.com/hyperscaleav/omniglass/issues/90)); dynamic membership and SCIM group mapping remain
  deferred.
