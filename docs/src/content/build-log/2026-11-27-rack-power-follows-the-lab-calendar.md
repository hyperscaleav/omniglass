---
title: "Rack power follows the lab calendar (#2777)"
date: 2026-11-27
refs: [2757, 2777]
---

The Ferrite PDU's outlets now draw what the room's day says: video, processing and amplifier gear idle in
standby, wake by the displays' rule (two minutes before a booked meeting someone attends, at the start of a
walk-in, five minutes after the end, nothing for a no-show) and draw more again during a call. The map from
outlet to gear is fixed in the profile, so no function was added and no device reads another. A new
`energy_mwh` counter (SNMP `.1.3.0`, `EnergyMwh` in the JSON document) accrues the total draw each tick. The
conference standard gains a power slot and the corporate manifest a PDU in it, so a corporate lab covers the
reader; plan counts follow. A PDU in a room with no calendar keeps its earlier random walk.
