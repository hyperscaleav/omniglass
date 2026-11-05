---
title: "The Polaris DSP reports its program mute and level (#1493)"
date: 2026-11-03
refs: [1493, 1266, 1372]
---

The lab's Polaris DSP 16s were collected by `snmp-generic`, which reads `model-number` and `uptime`
and nothing of what a DSP does. They now carry the class floor (#1266): a mute and a level read off the
DSP's own API.

The `polaris-dsp-16` simulator profile serves the DSP's program output, the bus that feeds the room,
from one state. Its SNMP agent answers the output's mute and level on the DSP's branch of the Polaris
arc, and a new HTTP route on the DSP's API mutes and unmutes the bus, so the value the driver
reads follows the toggle. The level drifts as a behavior inside a band, and a reboot or factory reset
returns both to their defaults.

The new `polaris-dsp-snmp` driver reads identity every five minutes and the program output every 30
seconds: `uptime`, `mute` and `audio-level`. A dedicated driver rather than OIDs on `snmp-generic`,
which has only the MIB-II set and no way to carry the vendor arc; this is the choice the amplifier
made. No new metric or property types were needed: `mute` and `audio-level` were already in the boot
seed. The product moves to the driver, and the lab attaches it with the community secret
`snmp-generic` uses.

The per-channel strips (gain, mute, peak) are served but not collected: per-channel values need a
driver to name an instance (#1252), and the DSP's full class set is #1360. The lab test states the
floor for the DSPs, every placed Polaris DSP 16 carrying a sampled `audio-level` and a `mute` sample,
until the lab-wide check holds it (#1371).
