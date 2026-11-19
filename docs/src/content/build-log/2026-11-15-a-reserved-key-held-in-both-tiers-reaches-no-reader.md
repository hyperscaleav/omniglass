---
title: "A reserved key held in both tiers reaches no reader (#1447)"
date: 2026-11-15
refs: [1447, 318, 1432]
---

The ingest registry and the SQL readers disagreed on a reserved key (a node-probe, platform or
command-lifecycle key) that an operator held as a `local` signal type before a release reserved it and
shipped the `og` row beside theirs. `ResolveNameLane` refused the name, so the node's writes landed
nowhere, while every reader going through `<table>_in_effect()` resolved it to the operator's row and
served its frozen pre-release values as the node's: the latest property, the availability strip's
transitions, and an endpoint's key pin among them. The three signal catalogs' `_in_effect()` functions
now return neither tier's row of a reserved name both tiers hold, in any of the three catalogs, so the
SQL search path and the registry give one answer. The reserved names live in a new `reserved_key`
table, which the boot seed reconciles to `writerclass.ReservedNames()` on every start: the list stays
generated from code, and a name the code stops reserving is deleted, the second table under the
storage page's reconcile-and-delete carve-out. `TestAReservedKeyHeldInBothTiersReachesNoReader`
failed before the change (all four readers served the operator's value) and passes after, with an
unreserved name still reaching the operator's row; `TestTheSeedMirrorsTheReservedKeysIntoSQL` holds the
table to the code's list across a boot and the next.
