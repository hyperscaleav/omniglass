---
title: "The Polaris amplifier is simulated and collected (#898)"
date: 2026-10-29
refs: [898, 1266, 1372]
---

The dev fleet's five Polaris Amp 4-100s had no simulator profile, so the lab placed them and collected
nothing from them. They now have one, and a driver of their own in place of `snmp-generic`, which read
only `sysDescr` and `sysUpTime`.

The profile serves two faces from one state. SNMP, on the amplifier's branch of the Polaris enterprise
arc (`.55540.3`), answers the chassis power, the protect state, the heatsink temperature, the fan, and
a channel table of output level, clip and load for its four channels. An HTTP JSON API behind the
Polaris header token enters and leaves standby and resets a latched protect. Temperature, fan and the
output levels drift as behaviors, each in a band, so a scenario timeline can drive them later.

The new `polaris-amp-snmp` driver reads identity every five minutes and the operating state every 30
seconds: `power-state`, `protect-state`, `temperature` and `fan-speed`. The last three are new to the
boot seed's canon, named for the measurement rather than the owner, so a DSP's chassis temperature can
land on the same name. The channel table is served but not yet collected: per-channel values need a
driver to name an instance (#1252), and a channel's load reaches the speaker it drives through the
fan-out (#1367).

The lab attaches the driver with the same community secret `snmp-generic` uses, and the amplifier
leaves `coverage.yaml`. The admin drivers guide's hand-written list of the shipped drivers, already
two short, now links to the set rendered from the seed.
