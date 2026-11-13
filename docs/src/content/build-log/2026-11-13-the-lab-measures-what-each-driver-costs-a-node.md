---
title: "The lab measures what each driver costs a node (#1871)"
date: 2026-11-13
refs: [1871, 1870, 1623]
---

`omniglass lab perf drivers` measures each shipped driver's cost per task on a node of its own: a
throwaway lab with no fleet, one node container per driver with the simulator beside it in a
container of its own (sharing the node's loopback, not its cgroup), and the node read at none, N and
2N of the driver's components. Memory and CPU per task are the slopes across the three steps, and a
step set that is not near a line is refused and listed as not measured, with the figure that failed.
Beside the cgroup's CPU per run sits the node's own `poll-cpu-time` (#1870) and the share of the run
it sees. The sweep writes `internal/labfootprint/drivercost/shipped.json`; `make gen` joins it with the
drivers the plan knows and renders the table on the scaling page, where a driver the committed sweep
lacks reads as not yet measured (ADR-1871).
