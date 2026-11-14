---
title: "The component_type walk is one statement (#803)"
date: 2026-11-12
refs: [803, 802, 1206]
---

Resolving a component_type's facts, and asking whether one type sits under another, now read the
whole ancestor chain in a single recursive query instead of a statement per level. The OAVC taxonomy
(#802) made the tree one to two levels deeper and the per-level walk made every generated-name create
and label render pay for it, so the label cost ceilings had been raised from 12 and 19 to 14 and 21.
They are back at 12 and 19. The fork rule is unchanged: each node's operator shadow comes back with
it and is applied per node in Go, so a forked middle node overrides only the facts it restates.
