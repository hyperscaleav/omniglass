---
title: "The lab measures what each driver costs a node (#1871)"
date: 2026-11-13
refs: [1871, 1870, 1623]
---

`omniglass lab perf drivers` measures each shipped driver's cost per task on a node of its own: a
throwaway lab with no fleet, one node container per driver with the simulator beside it in a
container of its own (sharing the node's loopback, not its cgroup), and the node read at none, N, 2N
and 3N of the driver's components. CPU per task is the slope across every step; memory per task is
the slope across the loaded steps, with the jump from the idle node to that line kept apart as a
one-off warm cost (an SNMP driver's working set went 15.8, 25.9, 27.4 MiB at 0, 400, 800 tasks: a
jump, then a slope, which one line through all three refused). A figure whose steps are not near a
line is listed as not measured with why, and the driver's other figure still stands. A driver whose
memory does not fit is swept again at four times the step, up to `--max-step-tasks`: at 400 tasks a
step, 13 of 20 drivers' memory sat inside the heap's own swing.
Beside the cgroup's CPU per run sits the node's own `poll-cpu-time` (#1870) and the share of the run
it sees. The sweep writes `internal/labfootprint/drivercost/shipped.json`; `make gen` joins it with the
drivers the plan knows and renders the table on the scaling page, where a driver the committed sweep
lacks reads as not yet measured (ADR-1871).

The committed sweep ran at 1,600 tasks a step on an 8-core host. All 20 swept drivers have their CPU
and 18 their memory; two SNMP drivers' memory stayed inside the heap's swing even at 6,400 tasks. An
SNMP or plain HTTP poll costs 5 to 10 KiB and 0.03 to 0.07 millicores a task (1.5 to 4.1 ms a run),
a device API 3.5 to 48 KiB and up to 0.24 millicores, and a held NVP session 60 to 168 KiB a
task at under 0.07 millicores. The nine drivers it does not sweep are the listener and the eight
alternate drivers no product defaults to, which #1943 costs by kind of work.
