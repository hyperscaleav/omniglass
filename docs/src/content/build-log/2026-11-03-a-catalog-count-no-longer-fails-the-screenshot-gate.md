---
title: "A catalog count no longer fails the screenshot gate (#1482)"
date: 2026-11-03
refs: [1482]
---

A PR that only seeds a driver or a catalog type touches no UI, yet it failed `make docs-shots-check`
on five shots (`action-rule-new`, `notification-target-new`, `templates`, `templates-dialog`,
`templates-system`) while `main` passed on the same base. The cause was one digit: the Catalog
subrail shows each registry's live row count (Drivers, Property types, and the rest), every one of
the five pages sits under that shell, and a seeded row moves a count by one. The diff was a 12 pixel
wide column in the rail, identical on all five.

The counts are the seed's, not the page's, so the baseline now paints over them. The mask
primitive gains a right-anchored declared box (`{ selector, width, align: right }`) because the
count is right-aligned at the end of a fixed-width row, so its right edge holds still and its left
edge moves with its digits, which the top-left box could not hold. A standing mask list
(`STANDING_MASKS` in `shotmasklib.mjs`) applies it to every shot wherever the rail renders, so a
shot added under the Catalog later does not need to remember it. The clean shots the docs embed
still show the counts live; the tolerance stays zero.

The alternatives were a fixed minimal catalog fixture (the shots would stop photographing the
catalog an operator sees, and the fixture would be a second seed to keep honest) and pinning each
picker's state (nothing but the rail was moving). The labels, the rows and the rest of the rail
stay in the baseline, so a renamed entry or a changed button still fails the gate.
