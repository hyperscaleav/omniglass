---
title: "Every measuring lab runs the corporate fleet at a stated size (#1719)"
date: 2026-11-10
refs: [1719, 1623, 1640, 1633]
---

A figure from one lab level now compares with the next: every level that measures runs the `corporate`
scenario at a stated size, on a ladder of 20 (the smoke run), 300, 1,000 and 3,000 systems, and the dev
fleet stays the showcase, never a measurement basis. `make test-lab`'s footprint and scale measure, and
`make lab-footprint` with no arguments, run corporate at 20 (`lab.MeasureScenario`, `lab.SmokeSystems`),
the smallest round size that seeds every product the manifest names; a unit test pins that 20 does and
16 does not. The footprint takes `-systems`, and since a corporate lab runs a node per site it reads the
lab's containers by label and sums the nodes into one row, so its table keeps its server, node and
Postgres rows. `make scale-lab` and `make scale-measure` default to 300, the ladder's first level.

`lab run` stamps the scenario and size on every container it starts (`omniglass.lab.scenario`,
`omniglass.lab.systems`), and both measures read them from the lab rather than from their own flags, so
a report always names the fleet it read; measuring a lab without them, or a dev lab, says so and stops.
The scale measure integration at corporate 20 now asserts every figure is read. The perf gate's refusal
of a run at another scenario or size lands with the gate itself: the spec is recorded on
[#1640](https://github.com/hyperscaleav/omniglass/issues/1640), since no compare exists yet. The lab
guide gains a table of which fleet each level measures.
