---
title: "Retyping a location in place is placement-checked (#251)"
date: 2026-11-12
refs: [251, 239, 1206]
---

`allowed_parent_types` was enforced on create and on move, so an operator could sidestep it by editing a
location's type instead of moving it: the console's edit form sends `location_type` on every save, and a
location retyped to a type that excludes its current parent only surfaced on the next move.
`UpdateLocation` now checks the final type against the parent the row already has whenever the
classification actually changes, and refuses it with the same 422 a move gets (`<type> is not allowed under
<parent type>`, or `<type> is not allowed at root` for a root location). A patch that restates the type the
row already carries is not a change and is not checked, so a grandfathered placement stays untouched until
an operator edits its type, which keeps ADR-0030's forward-only contract.

Storage and API tests cover a refused reclassify (by name and by uuid, with nothing written), one that
succeeds, a root location retyped to a type that forbids root, and the restated-type no-op.
