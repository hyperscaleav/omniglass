---
title: "The Lyra microphones are simulated and collected (#894)"
date: 2026-10-29
refs: [894, 1266]
---

The dev fleet's twenty Lyra microphones, ten Arc A2 ceiling arrays and ten HX-1 wireless systems, had
no simulator profile, so the lab placed them and collected nothing from them. They now have two, one
per product: the issue asked for one profile serving both, but the catalog serves variants of one kind
of component from a shared profile and never a different kind, and a ceiling array and a wireless
system are different microphones. Each also has a driver of its own in place of `snmp-generic`.

Both answer SNMP on a Lyra enterprise arc (`.55530`): mute and the input level in dBFS at `.1`, and
identity at `.5`. The HX-1's receiver adds its transmitter's battery at `.2`, which the Arc, wired
and powered over its network cable, does not have. A small HTTP API behind a Lyra header key mutes
and unmutes either. The level rides speech within a talker's band, and the HX-1's battery drains a
tenth of a percent a minute, about sixteen hours from full: behaviors a scenario can drive toward a
silent place or a dead transmitter.

`lyra-arc-snmp` reads `mute` and `audio-level`; `lyra-hx-snmp` reads those and `battery-charge`, the
receiver's percentage stored as the canon's ratio. A ceiling array has no battery, so the two cannot
share one driver without a fault on every poll. `audio-level` and `battery-charge` are new to the boot
seed's canon. The analog gooseneck, the Lyra GN-12, stays passive.
