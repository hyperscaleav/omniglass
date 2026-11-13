---
title: "The command health read keeps its unexecuted index (#1728)"
date: 2026-11-10
refs: [1728, 1626, 1631, 1630, 1731]
---

ADR-1626 expected #1631's deliverable index to replace `command_unexecuted_idx`, so #1728 asked
whether the drop was safe. It is not, and the index stays. The deliverable index is narrowed to
`status = 'issued'`, while the fleet's queue health read applies no status term (its expired counts
need the terminal commands that never ran, such as one stamped timed out), so the deliverable
predicate is not implied and the read cannot use that index at all.

Measured with EXPLAIN (ANALYZE, BUFFERS) on a seeded fleet of 30 nodes, 3,000 components and
303,331 commands (300,000 executed over 30 days, 3,331 unexecuted): the read touches 2,270 buffers
in 1.9 ms with the index and 4,980 buffers in 11.1 ms without it, its candidate scan falling back to
walking `command_component_ts_idx` with `ts` as a non-leading condition, a cost that grows with
history. The pull plans on `command_deliverable_idx` either way, unchanged by the drop. No migration
ships; ADR-1626 item 4 records the outcome.

Proof: `TestCommandHealthReadReachesTheUnexecutedIndex` replaces a test that accepted either index
on the planner's preference. It sets the competing indexes aside in a rolled-back transaction and
asks usability: the read's candidate scan reaches `command_unexecuted_idx`, and with that set aside
too, never reaches `command_deliverable_idx`. A migration dropping the index, and a read narrowed to
`issued` rows, each turn it red.

Thin cut: the same measurement showed the pull, at that scale, searching the deliverable index by
`ts` only and discarding other nodes' rows in a hash join, so it reads the fleet's live backlog
rather than its own; that is #1731.
