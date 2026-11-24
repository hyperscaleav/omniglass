---
title: "Hover explanations reach the keyboard (#2486)"
date: 2026-11-24
refs: [2486, 2485]
---

About 150 hover explanations used the native `title` attribute, which never shows on keyboard focus.
They are now the `Tooltip` wrapper: `Button` and `Popover` render one for their `title` prop (a disabled
button's tip rides a focusable wrapper so it can say why), explaining spans and links wear it directly,
and a truncated value is the one `Truncate` helper. Elements that only repeated visible text lost the
attribute. A guard test (`tooltip-guard.test.ts`) bars a bare `title=` on an element, and
`e2e/tooltip-keyboard.spec.ts` tabs the alarms page, a component page and the catalog to a tip. Tests
read a tip through `tipOf` (`test-tip.ts`), which focuses the element as a keyboard user does.
