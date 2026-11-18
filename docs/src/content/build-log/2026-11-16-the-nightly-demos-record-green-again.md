---
title: "The nightly demos record green again (#1858)"
date: 2026-11-16
refs: [1858, 1429]
---

Two merges left three demos failing in the nightly recording on main. Volume became a ratio in #1429, but the `websocket` and `http-sse` tapes still waited for the bar's raw 37 to arrive as a sample. They now wait for 0.37, while the bar itself is still set in its own 0 to 100 units. The `command-status` and `command-queue-health` tapes still recorded, but they issued `set-volume --value 42`, which the bar received as 4200. They now issue a fraction of the range. Breadcrumb crumbs became links in #1822, so step 14 of the `find-and-fix-a-place` walkthrough could no longer find an Explore button. That step now clicks the breadcrumb's Explore link, which is scoped to the breadcrumb so it cannot match the sidebar's.
