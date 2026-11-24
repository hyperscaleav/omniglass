---
title: "A component's Configure tab leads with Edit (#2439)"
date: 2026-11-24
refs: [2439, 2423]
---

The Configure face drew its action row (Edit, Delete, Cancel, Save changes) as a footer, so on a
component with every panel open Edit sat about 2,800 px down. The row is now the face's rail, the
first element on the tab in a page and in a blade. The Properties panel said "Nothing resolves for
this component yet" under a Reconciliation table listing properties, because it filtered out keys
that are only observed or told; the line now shows only when the owner has no rows at all. Effective
values hides platform-band secrets (the sixteen `sim-*` secrets every component inherits) behind a
"Show platform values" toggle, closed by default, and the Alarms panel states its explainer once.
Not done here: collapsing every section behind a one-line summary, and a "since" line for a healthy
component (it has no transitions read to draw one from). Tests: `ComponentDetail.test.tsx`
(Edit precedes the form on page and blade), `PropertiesPanel.test.tsx` (empty line),
`ResolutionPanel.test.tsx` (platform toggle).

The actions then moved onto the tab rail itself (#2439 review): a small Edit and a `⋯` menu holding
Delete at the rail's right end, Cancel and Save in the same place while editing, so the tab has no
separate action row. Tests: `ComponentDetail.test.tsx` (Edit and menu inside the rail, Delete
confirms, edit swaps to Cancel and Save).
