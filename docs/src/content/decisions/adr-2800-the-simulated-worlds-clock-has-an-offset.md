---
title: "The simulated world's clock has an offset, and a route reads it too"
date: 2026-11-27
status: Accepted
summary: "The simulator has a clock of its own, real time plus an offset, which every device and the problem executor read; a lab test sets it so a corporate room is mid-meeting without waiting for the calendar's hours. It moves what the simulated world reads and stamps, never the outpost's collection cadence or a timestamp Omniglass stamps, and it is the first, fixed-speed step of the accelerated clock (#1362)."
refs: [2800, 2757, 1362]
---

- **Date:** 2026-11-27 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/), [the lab](/developer/lab/)
- **Decision:** `sim.Clock` is the simulated world's clock: real time plus an `Offset`. The engine holds
  one (`Engine.SetClock`, real time when none is set) and every device it builds reads it
  (`Device.clock`), as do the scripted-problem executor and the pushers' engine. A lab test names the
  offset with `omniglass lab run --sim-clock-offset` (a Go duration), which sets
  `OMNIGLASS_SIM_CLOCK_OFFSET` on each site outpost's simulator. The offset moves every read of the
  world's time: the calendar funcs, `now`, a device's uptime anchor (seeded on the same clock, so
  uptime is unchanged by it) and the stamps a device writes in the world's own time (a reboot's
  `boot_epoch`, a webhook body's `ts`). It does not move the outpost's collection cadence, a timestamp
  Omniglass stamps (a sample's collection time, a request hit, the observation window) or a signature
  timestamp a webhook receiver checks against its own clock. A route's and a command's templates now
  read the device's clock for `now`, which amends ADR-2754.2's "a route's templates keep the process
  clock": with an offset a route stamping real time into an anchor the device's clock reads would
  read as a day of uptime. A session subject's push template still renders on the process clock.
- **Context:** Every #2757 lab test ran outside the corporate scenario's 08:00 to 18:00 UTC hours, so
  only idle agreement was proven end to end. A test that places "now" inside a chosen meeting proves
  the live case without waiting. The offset is the first, fixed-speed step of #1362's simulated clock:
  a speed factor joins `Offset` in the same `Clock`, the knob keeps its name and the seam is the one
  already there, so #1362 extends the type rather than replacing it. The world file was not the
  carrier: the world is what the scenario planned, the offset is how a run reads it, and one world
  file serves any offset.
