---
title: "The create rail renders through FormActionBar inline (#337)"
date: 2026-11-12
refs: [337, 1203, 1329]
---

Full-page create forms (`EntityCreateForm`) now render their Cancel and Create buttons through `FormActionBar` with the `inline` prop, the same rail the create page's template face draws. The hand-rolled row is replaced; the labels and the disabled rule are unchanged, and Cancel moves from the left edge to sit beside Create at the right, so both faces of `/<kind>s/create` align. Enter in a field still submits. `rail-ownership.test.ts` gains a fourth guard that bans the pattern outside of `FormActionBar` and `PanelFooter`, closing the gap it deliberately left open (#1329, ADR-0137).
