---
title: "A lab test can set the simulator's clock to prove a meeting live (#2800)"
date: 2026-11-27
refs: [2800, 2757, 1362]
---

The simulator has a clock of its own now: real time plus an offset, held by the engine and read by every
device, the scripted-problem executor and the pushers' engine. `omniglass lab run --sim-clock-offset 36h`
(a hidden flag, a scale lab's) sets `OMNIGLASS_SIM_CLOCK_OFFSET` on each site outpost, so a lab test places
"now" inside a chosen meeting of the plan's own calendar with no wait for the corporate hours. It moves
what the simulated world reads and stamps (the calendar funcs, `now`, uptime, a route's `now`) and
never the outpost's collection cadence or a timestamp Omniglass stamps. A route's `now` follows the device's
clock too, so a reboot's `boot_epoch` and the uptime that reads it agree under an offset. One lab test
picks a held, attended, sharing meeting of a corporate conference room, offsets the lab into it and reads
the bar in a call, the people counter at the attendees, the display awake, the switcher on the
conferencing input and the meeting platform's room in a meeting, live. It is the first, fixed-speed
step of the accelerated clock (#1362), which adds a speed to the same `Clock`. ADR-2800 records it.
