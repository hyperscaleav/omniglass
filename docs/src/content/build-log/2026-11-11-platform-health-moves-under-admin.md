---
title: "Platform health moves under Admin as Platform (#1767)"
date: 2026-11-11
refs: [1767, 1644]
---

Platform health's sidebar door left the top level, where it sat under Nodes, for the Admin group,
labeled **Platform**: whether Omniglass itself is well is administration, not day-to-day fleet work.
It keeps its route, `/platform`, and its `node:read` gate, so Admin now shows it, beside the
ungated Settings, to a `node:read` holder without the admin tier. The page keeps its title, and
Home's Platform tile is unchanged.

Every sidebar tab is now one word, and a nav test holds the rule over every group, leaf and child,
so the next tab that needs two words fails the build rather than slipping in.
