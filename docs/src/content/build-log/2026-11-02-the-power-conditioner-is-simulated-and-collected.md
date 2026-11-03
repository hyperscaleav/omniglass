---
title: "The power conditioner is simulated and collected (#1368)"
date: 2026-11-02
refs: [1368, 1266, 1359, 1252]
---

The dev fleet's power conditioner was the last component with no product, which left the lab nothing to
simulate for it and #1359 holding an allowance for it. It is now a Ferrite PDU 4C, a new fictional rack
power vendor, with a simulator profile and a driver of its own, and the allowance is gone: a dev seed
component with no product is an error.

The profile serves one state over two faces. SNMP, on the Ferrite enterprise arc (`.55560`), answers
the mains voltage in millivolts, the total current in milliamps, and an outlet table of name, state and
current for the four outlets, with identity beside it. A read-only JSON document carries the same state
in the shape a networked PDU publishes it (an agent block, a global measure, an array of outputs). Outlet
control is not modeled on either face. The mains and the outlet loads drift as behaviors; an outlet that is
off draws nothing, and the total is the sum of the outlets.

The `ferrite-pdu-snmp` driver reads identity every five minutes and, every 30 seconds, `uptime`,
`voltage`, `current` (both scaled from the agent's whole numbers to volts and amps) and four outlet state
properties. The outlets are four named properties, `outlet-1-state` to `outlet-4-state`, because a driver
cannot yet name an instance (#1252); when it can they collapse into one property with an instance per
outlet.

The class already existed: `power-device` (stem `power`) was in the component types, so no standard was
added. The component's generated name follows its type, so `device-1` is now `power-1`, and it keeps its
operator label "Power Conditioner".
