---
title: "Every blade seeds its edit drafts through the slot"
date: 2026-08-14
refs: []
form: bullet
---

- **Every blade seeds its edit drafts through the slot.** The thirteen blades still carrying the
  hand-written `createEffect(on(edit.editing, ...))` seeder (the twelve #748 listed plus
  SystemTypes, an omission its count missed) convert to `edit.bind({ seed })`: the slot runs the
  seeder on entering edit and again on `reseed()`, so the pattern is uniform when the next
  registry grows a `:restore` leg. Behavior unchanged per blade, the existing suites the gate;
  the page-detail flavor of the same hand-rolled seeding is #769 (#748).
