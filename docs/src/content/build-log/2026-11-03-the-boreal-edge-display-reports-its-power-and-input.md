---
title: "The Boreal Edge display reports its power and input (#1372)"
date: 2026-11-03
refs: [1372, 1360, 1266]
---

The lab's Boreal Edge displays were attached to `snmp-generic`, which read only `model-number` and
`uptime`, so a display could be switched off or pointed at another source and the console would not
notice. They now carry a driver of their own, `boreal-edge-snmp`, that reads the display's identity
every five minutes and, every 30 seconds, `uptime`, `power-state` and `video-input` from the Boreal
enterprise arc the Edge profile already serves.

The choice was a new SNMP driver over extending `boreal-edge-web`: the surface exists, the web admin
sits behind HTTP Digest and a login secret the lab does not attach, and the Boreal convention keeps
telemetry on SNMP (the Beam projector's driver is the precedent). Firmware is left out because the
profile does not serve it over SNMP.

No signal was missing from the canon, so the simulator gained none. `power-state` and `video-input` are
declared by the seed catalog (`property_types.yaml`) and by the driver's `emits`, which the seed validates
against it; they are live readings, so the products' property contract does not list them. One simulator fix rode along: the input route accepted any string, including
a source the display has no port for, and now rejects it with the same enum the profile lists. A POST
to the power or input route changes what the driver reads on its next poll. The lab test states the
floor, power state and input collected on every placed display, until #1371 holds it.
