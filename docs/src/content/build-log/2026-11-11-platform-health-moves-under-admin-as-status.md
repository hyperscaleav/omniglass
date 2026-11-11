---
title: "Platform health moves under Admin as Status (#1767)"
date: 2026-11-11
refs: [1767, 1644]
---

Platform health's sidebar door left the top level, where it sat under Nodes, for the Admin group,
labeled **Status**: whether Omniglass itself is well is administration, not day-to-day fleet work.
Status rather than Platform, which reads as vague, or Performance, which names only the timings.
It keeps its route, `/platform`, and its `node:read` gate, so Admin now shows it, beside the
ungated Settings, to a `node:read` holder without the admin tier. The page keeps its title, and
Home's Platform tile is unchanged.

Every sidebar tab is now one word, and a nav test holds the rule over every group, leaf and child,
so the next tab that needs two words fails the build rather than slipping in.
