---
title: "Platform health folds to a screen and has one name (#2446)"
date: 2026-11-23
refs: [2446, 1724, 1767]
---

Platform health was 13,215 px tall, ended in an unbounded simulator device table, and wore three
names: Status in the sidebar, STATUS in the top bar and Platform health on the page. It now reads
Platform health in all three, from one constant the nav test holds (ADR-2446).

Every section folds to one line, its verdict and the one number that matters (the end to end loss
and its worst hop, a server's open alarms and the panels they flag, how many outposts are up, the
worst gap the simulator sees), and opens on demand. The open sections are the `open` query
parameter, so a reload keeps them, and a closed section mounts nothing, so a folded page binds no
series stream. The summaries are pure functions of the view the page already holds; the page adds
no figure and no threshold.

The device table is a filtered page of 25 rows, with each device's system and place beside its
name, so two `amp-1` read apart (#1724; `GET /views/platform/simulation` carries `system` and
`place` per device), a State column that is never blank, and cells that line up with the Device
column. The second Lab instance badge is gone (the shell's banner says it), and **Queue depth** and
**Busy workers**, which only ever read "not wired yet", are removed: the outpost's command queue
depth is already a column of the outposts table.
