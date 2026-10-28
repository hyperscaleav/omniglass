---
title: "Every lab component is simulated or accounted for (#1359)"
date: 2026-10-28
refs: [1359, 1266]
---

The dev seed places 214 components, 213 of them always and the UCC service when the simulator is on.
The lab collects from 123 of them. A test now accounts for the rest:
- 75 wait on a simulator profile, each product listed with the issue that will add it;
- 15 are passive (ceiling speakers and analog goosenecks with no network interface), each listed with
  how the fleet watches it instead, which later lands on them as their own telemetry through the
  fan-out (#1367);
- one is the fleet's example of a component with no product, which becomes a networked power
  conditioner (#1368).

A component of a new, unlisted product fails the test with its name. So do a listed product that has
since gained a profile, a "passive" product that has a driver, and a productless entry whose component
has since gained a product.

This is the first step of making the simulated fleet complete before it is scaled (#1266, then
#1357). Each profile slice from here shrinks the list of 75; the fleet's recording service, a software
service with no driver, is on it and gets its profile in #1363.
