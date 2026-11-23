---
title: "The lab's alarms are true: its faults are causes the rules judge (#2434)"
date: 2026-11-23
refs: [2434, 909]
---

The dev fleet hand-inserted two alarms with no rule behind them (DSP 1, "No route to host on the
control interface", and Display 1, "Lamp hours approaching end of life") while the simulator served
both devices healthy, so the first alarm a demo opened was contradicted by the page it sat on. The
seed now raises no alarm. It seeds each fault's cause, under a new `faults` fixture section, and the
rule that judges it, under `event_rules`: beside the simulator, DSP 1 is unplugged (the official
`reachability-lost` rule opens a High alarm) and Display 1 answers 400 ms late (the fleet's own
`response-slow` rule, over 250 ms held 15 s, opens a Warning). Each impairment is set once, as the
device is wired, so a fault a presenter repairs on the Simulation page clears its alarm through the
rule and stays repaired across a restart. With no simulator (the docs' screenshots) the cause is the
reading the fault began with, landed once through the ingest. The lamp-hours story became a slow
link: no simulated display reports its lamp, and pinning a value is #909.

The lab's readiness, the lab-wide data check and the DSP floor treat an impaired device as broken on
purpose, and `make test-lab` gains a subtest that every open alarm has a rule, its event, a rule still
firing and (for reachability) a component reading down, and one that lifting a fault clears its
alarm. The degraded and storm scenarios keep their hand-raised alarm storm, now from
`alarm_templates`. The docs capture waits for every rule streak to settle before shooting.
