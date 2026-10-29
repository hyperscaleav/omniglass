---
title: "The Boreal projector is simulated and collected (#899)"
date: 2026-10-29
refs: [899, 1266]
---

The auditorium's Boreal Beam 340 had no simulator profile: a projector is not a display, so the Edge
profile could not stand in, and the lab placed it and collected nothing from it. It now has a profile
of its own and a driver in place of `snmp-generic`, which read only `sysDescr` and `sysUpTime`.

The profile follows the Boreal convention the Edge displays set. A web admin behind HTTP Digest
reports the projector's state as JSON and sets its power, its shutter and its input; SNMP on the
Boreal enterprise arc answers the same state, the power state where every Boreal agent keeps it and
the projector's own branch at `.55520.6`: light-source hours, shutter, input and temperature. The
light source ages only while it is lit, a minute's hours a minute, a behavior a scenario timeline can
run faster to walk the source toward its service threshold.

The new `boreal-beam-snmp` driver reads identity every five minutes and, every 30 seconds,
`power-state`, `video-input` and two signals new to the boot seed's canon: `light-source-hours`, the
number a projector is serviced by, and `shutter-state`, open or closed, since a closed shutter blanks
the picture with the projector still on. The lab attaches it with the community secret every SNMP
driver there uses, and the projector leaves `coverage.yaml`.
