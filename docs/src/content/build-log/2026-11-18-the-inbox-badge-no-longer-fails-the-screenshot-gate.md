---
title: "The inbox badge no longer fails the screenshot gate (#2196)"
date: 2026-11-18
refs: [2196]
---

`freshness` went red on main and stayed red, on pushes that touched no UI. One shot drifted,
`platform-health-hop`, and only in a 16 pixel square at the top right: the inbox bell's unread
badge, a "1" in the fresh capture and nothing in the committed baseline. The lab raises a
notification partway through a capture, so the count belongs to the run, not the page. A shot
taken well after it (`templates`) always has the badge, and one taken close to that moment
flips between runs.

The bell is now a standing mask, painted on every shot that renders the top bar. The badge is
pinned past the bell's own corner, so neither a mask on the bell (its edge stays live) nor a mask
on the badge (nothing is painted when there is no badge) holds. The mask primitive gains a padded
box, `{ selector, pad: N }`, the element's bounding box grown on every side, and the bell takes
8px. The clean shots the docs embed still show the badge live; the tolerance stays zero.
