---
title: "Explore becomes an outline of places (#861, slices #866 to #869)"
date: 2026-10-04
refs: [861, 866, 869]
---

The renderer library did not survive use. Four drawings of one fleet, a cut the operator had to
reverse-engineer from the cards, and controls that came and went with the renderer made the page
hard to navigate and opinionated about how to look; the ask was one simple way to see the stack
at a glance and drill in as needed. Explore is now an outline: the place tree as an ARIA tree,
each place wearing the system that sits there (standard and verdict on its own row, the
components directly beneath), single-child chains folded into one row, and a collapsed row that
says what it holds in the registry's own type labels and counts its systems by verdict in four
fixed slots. The rules the library had earned carry over (depth is the customer's, counts rather
than a worst-wins rollup, a predicate over live state rather than a scope); everything that asked
the operator to choose how to look is gone. ADR-0137 is rewritten in place, since none of it
shipped.

`lib/outline.ts` is the one pure model, tested with a plot, sector and coordinate fixture beside
the shipped campus tree, and `components/Outline.tsx` renders it. The cut, the budgets, the
presets, the mosaic and matrix cores and their renderers are deleted (about 4,000 lines with their
tests), as are the index halves of the Locations, Systems and Components pages: each route is now
its workspace or, at `/<kind>s/create`, a shared `CreatePage`, and `TreeList` keeps only the
options Files uses. Find things in your fleet folded into the Explore guide, and its address
redirects there.

The e2e rewrite found three things the unit suites could not. A folded row's **+** could only add
under its innermost place, so a campus of one building could never gain a second from the outline;
the add menu now offers a section per place the row joins. A fleet with no locations showed "No
locations yet" while a component created nowhere existed, invisible. And beside an open sidebar
the Name column measured 16px at a 640px window: the columns followed the window, not the outline,
so they now answer container queries and give way in order (standard, then type, then detail). The
rollup's own live capture at 640px then showed that passing a width check is not reading: a folded
row cut its outer path and its own name equally ("Air... A."), and a room two levels down kept one
letter. The outer path now gives way first and steps aside at the narrowest, the indent halves,
and Health drops its empty slots there; the e2e asserts the row's own name is not clipped. The
sidebar that keeps a third of a 640px window is the shell's, filed as #871.

An adversarial review of the view then found nine more, each fixed behind a test proven red
first. The Detail column's alarm text never rendered, because the health report names an alarm's
component by uuid and the page looked it up by name. A collapsed place holding one system showed
only that system's verdict and hid an outage in a child place. Every toggle rebuilt every row and
dropped keyboard focus (rows now iterate stable string keys). Keys pressed inside the portaled add
menu bubbled to the row behind it and opened the wrong panel. The add menu had no keyboard path
(it is `+` now), the unplaced node's filter result opened a panel for a place that does not exist,
health reads fanned out for every system ever opened (now only those on screen, and none while
the filter shows results), results had no keyboard way to open a panel (Space), and `?node=`
missed a folded outer place, a system placed out of sight, and a name shared by two kinds.
