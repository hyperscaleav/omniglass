---
title: "Retyping a location checks its children's placement too (#1854)"
date: 2026-11-23
refs: [1854, 251, 239]
---

Retyping a location in place was checked against its parent (#251) but not against its children, so
a building holding a floor could become a room and strand the floor under a parent its type refuses.
`UpdateLocation` now also checks every direct child when `location_type` actually changes: each
child type's `allowed_parent_types` must admit the new type, or the request is the same 422 as a
move (`<child type> is not allowed under <new type>`) and nothing is written. A restated type stays
a no-op, and grandchildren are not checked. ADR-0030 records the rule.
