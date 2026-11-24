---
title: "The catalog's pages share one header, form order and footer shape (#2431)"
date: 2026-11-23
refs: [2431]
---

A live audit of the twenty catalog pages found small drifts that together made the catalog look
unfinished. Each is fixed in the shared primitive, once.

- The overview's cards size to their content: daisyUI grows a card-body paragraph, which handed the
  tallest card's surplus to the copy and pushed Telemetry's counts 400 px down. The copy no longer
  grows and the grid aligns cards to their start.
- A list with no New button holds the same bar height as one with it (the filter row reserves a
  button's height), so Templates and Secret types sit level with their siblings.
- While chips filter, the filter bar reads `n of m`. A Templates slot opened from the sub-rail
  therefore shows its kind chip and `5 of 22`, not a bare list that disagrees with the rail.
- An empty list shows its empty state alone, with no column headers above it (calc rules, empty).
- Every catalog create form opens on Label, then the derived Name, then its kind-specific fields,
  through one `IdentityFields` component with one set of hints. Five forms that led with Name, and
  two that wedged a data type between the pair, now match.
- A locked official row draws one note, "Official, read-only", in place of greyed Edit and Delete.
  Secret types, whose API has no write route, say so in the same note. A forkable official row keeps
  its live Edit and shows the note in place of the dead Delete.
- Below 1100 px the catalog sub-rail folds behind one button naming the current section.

Guards: `catalog_create_order.test.tsx` (the field order on every catalog page),
`catalog_readonly.test.tsx` (no disabled button on a kind whose generated route table has no write
route), and the footer-shape and note cases in `BladeStack.test.tsx`. The event rule form keeps its
own order for now (its own issue). The `/log-types` stub, item 3 of the issue, is removed by #2505.
