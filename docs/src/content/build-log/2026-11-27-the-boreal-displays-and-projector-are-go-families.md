---
title: "The Boreal displays, projector and player are Go families (#2832)"
date: 2026-11-27
refs: [2832, 2826, 2829]
---

The Boreal Edge display, the Beam projector and the Cast player are now Go packages under `sim/device/`
(`borealedge`, `borealbeam`, `borealcast`), the first families of the Go-families epic (#2826), and their
three YAML profiles and templates are deleted. The Edge's five sizes are five rows of one typed table, each
reporting its own model: a 98-inch no longer calls itself an Edge 86 on its dashboard. JSON is typed structs,
control routes are one `httpapi.Control` declaration that also lists the route's quick sets and registers
the paths for the console's fault chip, SNMP is typed scalars that name the state path they read, the
dashboard is `html/template` over a typed view struct, and behavior is `sim/behave` primitives joined to the
family's state by `sim/device/internal/bind` (the new `Drift`, `Wear` and `PlaylistAdvance` beside the room
primitives). The engine gained what a family needed to be listed and driven as a profile is: a handler
writes several paths as one change and reads its defaults and clock, a tick reads the building's hours and
is fired and listed by name, a family's OIDs answer the console's fire, and the console's behaviors list
includes a family's ticks.

Readings are identical because they were recorded: a script ran against each profile before it was deleted
and its transcript (statuses, headers, bytes, state, SNMP types and values, seeded drift, a pinned room day
with and without a calendar, and the console's listing) is the testdata every family test replays
(ADR-2832, which lists the two accidents of the old templates the families correct). That recording, the
`spectest.Conform` suite and the worked example in the simulation architecture page are the pattern the next
families follow.
