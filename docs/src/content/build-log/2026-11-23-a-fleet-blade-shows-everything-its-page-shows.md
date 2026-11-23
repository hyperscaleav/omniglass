---
title: "A fleet blade shows everything its page shows (#2423)"
date: 2026-11-23
refs: [2423, 872, 2115]
---

A component, system or location opened in a side panel used to be a separate, reduced
implementation (`EntityBlade`'s three bodies), so an operator had to Expand to the page to read
vitals, collection, members, history or configuration. Each kind now has one body,
`ComponentDetail`, `SystemDetail` and `LocationDetail` (`{ id, host }`), mounted by its page and by
its blade alike; `EntityBlade.tsx` keeps only the blade definitions (ADR-2423, reversing ADR-0139's
glance rule).

The host holds three things. The tab lives in the page's `?tab=` or in a blade's own signal
(`lib/tabstate`, which also folds the three pages' duplicated legacy-tab and edit-intent
resolvers), so switching a blade's tab never rewrites the page beneath it, and a rooted outline in
a blade filters the same way. Edit answers `?edit=` only on a page. Delete, now in the Configure
footer in both hosts, closes a blade's stack or sends a page to Explore. Sections lay out by
container query, and fleet blades take a wide (about 720 px) variant, so the same body reads as one
column in the blade and two on the page. Each host gained what the other had: the page's Why rows
carry the rule and Troubleshoot, the system's Why controls sit in one right-aligned group, and the
location's card shows its health and since-when and what it holds. The alarm rows use #2115's
`AlarmActions`. `fleetBlades.guard.test.ts` fails if a kind registers a blade body of its own
again.
