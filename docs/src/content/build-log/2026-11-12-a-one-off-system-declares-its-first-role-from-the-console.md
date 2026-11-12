---
title: "A one-off system declares its first role from the console (#321)"
date: 2026-11-12
refs: [321, 1203, 266]
---

A system with no standard inherits no roles, and its Roles panel had no way to add one even though the
panel's own empty copy says a role is declared "directly on the system". In edit mode (and holding
`system:update`) the Ad hoc group now carries an add row, including from the empty state, and each ad
hoc role gains edit and withdraw controls. Inherited rows stay read-only: they are edited on the
standard. A name the system's standard already declares comes back from the server as a 409, which the
add row shows inline in the server's words, leaving the draft and the panel in edit.

`web/src/lib/system_roles.ts` gains `setSystemRole` and `deleteSystemRole` over the existing
`PATCH` and `DELETE /systems/{name}/roles/{role}`; no API changed. `RoleEditor`'s declare row and
in-place edit form are now owner-generic (`RoleDeclareRow`, `RoleEditForm`, keyed by a `standard` or
`system` owner, each holding its own draft and error), so the standard's panel and the system's share
one set of controls and differ only in which write they call. The standards behaviour and its tests are
unchanged.
