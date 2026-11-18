---
title: System map labels fit the box and do not collide (#1072)
date: 2026-11-13
refs: [1072, 1074, 873]
---

A system map marker's label no longer clips at the map's edge or overprints a neighbour at any
width. #1218 anchored edge markers inward by fractions of the box, which fixed desktop widths only:
a label is up to 112 px wide, so at phone width a marker at x = 0.2 still spilled, and close markers
(the Auditorium's podium and stage microphones) stacked their labels.

`markerAnchor(x, y, box, label)` in `lib/system_map.ts` now decides in pixels from the rendered box
and the label's estimated size (`labelSize`, monospace glyph width capped at 112 px and two lines),
keeping an 8 px gutter: the same 0.2 position hangs inward in a 320 px box and stays centred in a
1200 px one, and a label flips above its icon when it would run past the bottom, not at a fixed 0.85.
`layoutMarkers` is a pure pass over the whole marker list in declaration order: each marker takes its
anchor if its label stays inside the box and clear of the other markers' icons and the labels already
placed, otherwise its label's other side, then the other horizontal anchors, and when nothing is clear
the label collapses to the icon alone (the text stays in the tooltip, and an empty ring gains an
`aria-label`). An earlier marker never moves for a later one. `SystemMap` measures its box with a
`ResizeObserver` and feeds the width in, so the geometry lives in one tested function that jsdom and a
browser agree on. Icons that sit on top of each other are the declaration's to fix; the layout moves
only labels.

Proven by unit tests of the anchor (320 px against 1200 px), the label estimate, and the layout over
the degraded Auditorium's declared positions at 320, 480, 672 and 1280 px (every label inside the
box, none over another label or icon, deterministic), and by a component test that renders that
marker set at 320 and 1280 px and asserts the same on the rendered label boxes.
