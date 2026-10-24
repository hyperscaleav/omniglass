---
title: "A calc rule derives component samples (#996)"
date: 2026-10-22
refs: [996]
---

The second slice of epic #525 ([ADR-0148](/architecture/decisions/#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer)).
`calc_pending` and `calc_rule_state` (migration `20261021070000`) hold a rule's pending work and its last
outcome per owner. The sample-write path (`writeMetricSamples`, `writePropertySamples`, shared by the
ingest and the calc write) marks every enabled component rule reading the batch's keys on an owner its
selector picks, before it inserts anything, skipping the rule that produced the batch. The server's calc
tick (`internal/bus/server.go`, every second, 500 marks a pass under a 10s deadline) drains marks with
`FOR UPDATE SKIP LOCKED`, one transaction each: reads the inputs' current values (every instance for a
named reducer, the aggregate instance for an expression, never intended), reduces (`rules.CalcProgram.Reduce`:
the eight reducers and the compiled calc expression), and writes a changed result as a calculated sample
naming the rule and its version, where event rules judge it. A missing input writes nothing and is named
in the state row. Proven: a cross-key rule writes once the tick runs and an unchanged input writes no
row; an event rule fires on a derived property; a chain recomputes downstream and a rule never re-marks
itself (a mutation that dropped the skip re-marked it forever); two replicas compute every mark once and
a restart mid-backlog loses nothing; and end to end, a node's tcp probe on a simulated device is folded
by a CLI-authored rule into a calculated metric whose event rule raises an alarm. System rules wait for
#997. Review hardened it before merge: a mark the
batch finds already present is updated, not skipped, so the batch holds it until commit and the tick
cannot compute from the old value (a skipped conflict lost the change); the ingest retries its whole
transaction on a deadlock with the tick (`sampleWriteAttempts`); a failing mark is recorded and deferred
30s while the pass goes on, a held rule's marks are passed over, and a panicking pass is recovered; a
rule's create, enable or edit marks the components already holding its inputs and resets its state; the
output is written on the lane the rule holds it by; and a reducer folds named instances (the aggregate
only when alone) and skips instances stale past an hour, naming them in the state.
