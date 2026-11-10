---
title: "Every measuring lab runs the corporate fleet at a stated size"
date: 2026-11-10
status: Accepted
summary: "Accepted: every lab level that measures runs the corporate scenario at a size on one ladder (20 for the smoke run, then 300, 1,000 and 3,000 systems), and a figure compares only with the same scenario at the same size. The dev fleet is the showcase, never a measurement basis. A lab stamps its scenario and size on its containers, every measure reads them from there, and the lab footprint sums a corporate lab's per-site nodes into its one node row."
refs: [1719, 1277, 1640, 1633]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [lab](/guides/lab/), [test-driven](/contributing/test-driven/), [scaling](/architecture/scaling/)
- **Decision:** (#1719; extends
  [ADR-1277](#adr-1277-the-lab-footprint-measures-a-running-lab-per-container-and-gates-nothing).)
  1. **One fleet shape, one ladder.** `make test-lab` and `make lab-footprint` run corporate at 20
     systems (`lab.MeasureScenario`, `lab.SmokeSystems`); `make scale-lab` and `make scale-measure`
     default to 300; the ladder is 300, 1,000 and 3,000. 20 is the smallest round size past the last
     one that drops a product the manifest names, so the smoke run exercises every simulator
     profile the ladder does.
  2. **The lab names its own fleet.** `lab run` labels every container with
     `omniglass.lab.scenario` and `omniglass.lab.systems`. `lab measure` and the footprint read the
     scenario and size from those labels, never from their own flags; `lab measure` refuses a lab
     without them, or a dev lab, which has no size.
  3. **The footprint keeps three rows.** A corporate lab runs a node per site, so the footprint
     finds its containers by the project's label and sums every node's CPU and working set into
     one node row beside the server and Postgres.
- **Context:** the footprint measured the dev fleet, whose broken devices and unassigned
  identities give it rows, a cost and a loss that share no shape with a deployment, while the scale
  measure ran corporate at whatever size it was asked. No figure from one level compared with the
  next, and the perf gate (#1640) needs a report it can trust to name its fleet.
- **Rejected:** a per-node row in the footprint (its table would grow with the site count, and no
  level reads one node's share); taking the size from the database's count (a seeded fleet the
  operator edits would change the size a figure is quoted at).
