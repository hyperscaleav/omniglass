---
title: "The Meridian switcher and extender are simulated and collected (#895)"
date: 2026-10-29
refs: [895, 1266, 1252]
---

The dev fleet's Meridian signal path, three MX 84 presentation switchers and four XL-1 extenders, had
no simulator profile, so the lab placed it and collected nothing from it. Each product now has a
profile and a driver of its own. The issue asked for one profile serving both, but a profile serves
variants of one kind of component, and a switcher and an extender are different kinds.

Both answer SNMP on a Meridian enterprise arc (`.55550`). The MX 84, eight inputs and four outputs,
keeps an input table of signal presence at `.1` and an output table at `.2`, each output's routed
input and whether that input carries a signal; an HTTP JSON API behind a Meridian header key routes an
input to an output and plugs a source in or out. A behavior keeps every output's signal following its
input, so a laptop going to sleep darkens each output showing it within a tick. The XL-1 receiver
reports the link between its pair and whether a signal arrives over it, at `.3`.

`meridian-mx-snmp` reads output 1, the room's main display feed: its `video-input` and its
`signal-state`; the other outputs and the input table wait on a driver naming an instance (#1252).
`meridian-xl-snmp` reads `link-state` and `signal-state`. Both properties are new to the boot seed,
and `video-input` gains `hdmi4` to `hdmi6` for the switcher's eight inputs.
