---
title: "A component's page shows and edits its dependencies (#2428)"
date: 2026-11-23
refs: [2428, 2452, 1648, 1652]
---

The dependency API (`list-component-dependency`, `create-component-dependency`,
`delete-component-dependency`) had no page behind it. A component's Overview now carries a
Dependencies card, `DependenciesPanel` in its own file: Upstream (what the component needs, with
each upstream's health, its kind and outlet, and a "holding this" mark when that upstream's gate is
closed on the component's endpoints) and Downstream (what needs it, one row per dependent with
every kind it depends by). Rows link to the other component.

Adding goes through a filter box over a native select of the fleet (the component itself is never
offered) plus the kind, and the outlet on a power edge. The server's refusal of a cycle, a repeat
or a missing outlet shows in the form, in its own words. Removing an upstream asks first, through
the console's `ConfirmDialog`. A viewer (no update on the component) sees both lists read-only.

No API change: the list route already answers both directions in one read. The dev seed gains a
second downstream of Boardroom A's video bar (the room display, reached through it), so the bar
has one upstream and two downstreams. The "holding this" mark reads the upstream reason on the
component's own endpoint gates, so it names the root of a chain; naming the holder in the Why strip
is deferred to #2452 (recorded on #2428) because that row is being reworked by #2423.
