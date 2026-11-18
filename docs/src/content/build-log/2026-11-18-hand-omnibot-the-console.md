---
title: "Hand omnibot the console: the tray, alt-click and one-click triage (#2122)"
date: 2026-11-18
refs: [2122, 2121, 2026]
---

A person now hands omnibot what they are looking at (ADR-2122). Alt-click on a row of any list, tree,
blade or alarm adds its subject to a tray above omnibot's composer, shift-alt-click a range, and ⌘J
hands over the page as its route and arguments. The next prompt carries the tray as AG-UI context,
references only (never a uuid or what the screen showed), which the server frames as quoted data
ahead of the message. An alarm in the tray triages in one click, from its chip or the command
palette: the run names the embedded `triage-alarm` skill in `forwardedProps.skill` and the server
renders its prompt. The stamp rides the shared primitives rather than the ViewResult renderer, which
is still Design (#523).
