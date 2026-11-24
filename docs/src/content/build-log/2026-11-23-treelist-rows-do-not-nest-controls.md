---
title: "TreeList rows no longer nest controls (#974)"
date: 2026-11-23
refs: [974, 965]
---

The route probe reported axe `nested-interactive` (serious) on `/files` and every other TreeList
consumer: each row was a `<tr role="button" tabindex="0">` holding the expand toggle and the
Open / Edit / Delete buttons. A `role="button"` element must not contain focusable descendants.

The fix is in the primitive. The row is no longer a button; the name cell is a real `<button>`
that opens the node, so a keyboard user tabs to one primary control and then the row's actions.
Pointer users keep click-anywhere through the row's `onClick`. `TreeList.test.tsx` asserts no
`role="button"` element holds a focusable descendant and that the name control opens the node.
