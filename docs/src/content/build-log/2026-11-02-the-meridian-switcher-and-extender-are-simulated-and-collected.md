---
title: "The Meridian switcher and extender are simulated and collected (#895)"
date: 2026-11-02
refs: [895, 1266]
---

The dev fleet's seven Meridian devices, MX 84 presentation switchers and XL-1 AV extenders, had no
simulator profile, so the lab placed them and collected nothing from them. They now have two, one per
product: the issue asked for one profile serving both, but the catalog serves variants of one kind of
component from a shared profile and never a different kind, and a switcher and an extender are
different components (`TestEveryDeviceProfileNamesASeededProduct` holds a profile to one component
type). Each also has a driver of its own in place of `snmp-generic`, which reads only `sysDescr` and
`sysUpTime` and so cannot reach a signal of the device's class; like every driver, each is named for
its product (`meridian-mx-snmp`, `meridian-xl-snmp`), not the device class.

Both answer SNMP on a Meridian enterprise arc (`.55550`): the switcher's branch at `.1`, the
extender's at `.2`, identity at `.5`. The switcher reports its routed input, the signal on that input,
and a table of the signal on each of its four inputs. The extender reports the link to its transmitter
and the signal on its one input. A small HTTP API behind a Meridian header key reports the same state
as JSON; the switcher's `POST /api/route` changes the routed input, and the signal it reports for the
routed input follows the new route at once and then each tick, so a source pulled from the routed
input shows as no signal. A route to an input the switcher does not have is refused and changes
neither the routed input nor its signal. Both reboot over HTTP, which restarts uptime and retrains the extender's
link.

`meridian-mx-snmp` reads `video-input` and `signal-state`; `meridian-xl-snmp` reads
`link-state` and `signal-state`. `signal-state` (present or absent) and `link-state` (up or down) are
new to the boot seed's canon. The per-input signal table is served for the class's fuller signal set
(#1360), which waits on a driver naming an instance (#1252); the drivers read only the routed input's
signal today. Both products leave `coverage.yaml`.
