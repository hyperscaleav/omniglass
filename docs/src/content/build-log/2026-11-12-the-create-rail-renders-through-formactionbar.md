---
title: "The create rail renders through FormActionBar inline"
date: 2026-11-12
issue: 337
---

Full-page create forms (`EntityCreateForm`) now render their Cancel and Create buttons through `FormActionBar` with the `inline` prop, the same rail contract Drawers use. The hand-rolled button row at EntityForm 701-708 is replaced; the labels, layout and disabled conditions are unchanged. `rail-ownership.test.ts` gains a fourth guard that bans the pattern outside of `FormActionBar` and `PanelFooter`, closing the gap it deliberately left open (#1329, ADR-0137).
