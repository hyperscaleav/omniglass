---
title: The docs site is laid out by audience
date: 2026-11-18
refs: [2155, 2153]
---

The sidebar now has one section per reader: Start here, Operator, Admin, Developer, Deployment,
Architecture and Reference, each with a landing page that says who it is for and what it holds.
Each section's sidebar is generated from its directory, so a page joins a section by being written
there and orders itself with `sidebar.order`. The old pages sit in a collapsed **Being rebuilt**
group and stay the reference until the PR that rebuilds their section deletes them (ADR-2087.2);
the unbadged architecture pages set `sidebar.hidden`, so the Architecture section lists only the
overview, the status grid, the roadmap, the decision log and the build log.
