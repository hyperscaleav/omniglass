---
title: "Aiming an endpoint at a simulated device adds no phantom simulation (#2461)"
date: 2026-11-24
refs: [2461]
---

Every component with an endpoint on a simulator's outpost read as a simulated device, so a hand-built
component aimed at a simulated device's address was handed to the simulator as a second device at the
same address: a Simulation row with 0 hits, status failed and the wrong profile, and a "Simulation:
failed" card beside a green collection. `OutpostSimDevices` now keeps one device per address (the
earliest component owns it) and records later ones as `Aliases`, so the simulator is never handed
them. The component's simulation read resolves an alias to the owner's device and carries `served_by`;
its card says so and its drawer opens the owner's. Remove now deletes the component's endpoints first
(the server refuses to delete a component that owns one), after one confirmation that lists them.
Not done here: the Add simulated component form's product label and system field (filed against #2461 for a later slice). Tests:
`TestOutpostSimDevices`, `TestComponentAimedAtASimulatedDeviceIsServedByIt`, `Simulation.test.tsx`.
