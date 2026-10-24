---
title: "The breadcrumb is the walk; the ladder goes"
date: 2026-08-19
refs: []
form: bullet
---

- **The breadcrumb is the walk; the ladder goes**
  ([#630](https://github.com/hyperscaleav/omniglass/issues/630)). The zoom ladder duplicated
  what the console's breadcrumb already does, and the fleet pages now use plain breadcrumbs:
  the trail ends at the parent (the current page is the title, never repeated as the last
  crumb), and a component's trail runs Fleet, its places, its primary system. Titles stop
  repeating what the body says (a location's type chip, a component's product panel).
  `ZoomLadder` and `lib/zoom` are deleted; the glossary's zoom-ladder term retires (#630).
