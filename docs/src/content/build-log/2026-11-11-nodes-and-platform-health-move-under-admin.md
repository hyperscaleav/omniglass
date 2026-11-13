---
title: "Nodes and Platform health move under Admin (#1767)"
date: 2026-11-11
refs: [1767, 1644]
---

Nodes and Platform health left the top level of the sidebar for the Admin group: enrolling a node
and reading whether Omniglass itself is well are administration, not day-to-day fleet work
(ADR-1767). Platform health's door is labeled **Status**, rather than Platform, which reads as
vague, or Performance, which names only the timings. Both keep their routes, `/nodes` and
`/platform`, and their `node:read` gate, so Admin now shows them, beside the ungated Settings, to
a `node:read` holder without the admin tier. The pages keep their titles, and Home's Platform tile
is unchanged.

Every sidebar tab is now one word, and a nav test holds the rule over every group, leaf and child,
so the next tab that needs two words fails the build rather than slipping in.
