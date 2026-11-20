---
title: "Every location field picks from the location tree (#2301)"
date: 2026-11-20
refs: [2301]
---

`TreeSelect` already drew the location tree, but four location fields had drifted to a flat native
`<select>` (the outpost blade and create form, and both simulation add forms), the location edit
face's parent was a flat list filtered by the type's allowed parents, and the forms that did use the
tree each built its location items by hand, so they disagreed on sibling order.

`LocationSelect` is now the one location field. It reads the locations and their types itself and
builds the options through `locationTreeNodes`, so every field nests by parent and orders siblings by
type rank then label. Each caller keeps the address it already committed (uuid, name, or dotted
path), so no request changes. `TreeSelect` gained disabled options: the parent picker shows a location
its type may not sit under, muted and unpickable, instead of dropping it and orphaning its children
to the top of the list. The outpost blade drops `bindSelectValue`, since the tree selects from value
and options together, and the outpost create form moves onto `FieldRow`, which labels the tree's
trigger.

A static guard fails on any location field built another way, and it found two the issue's sweep
missed: the Secrets and Variables owner pickers when the owner kind is a location. Type-to-filter
inside the tree is #2302 and lands in `TreeSelect`, so every tree field gains it at once.
