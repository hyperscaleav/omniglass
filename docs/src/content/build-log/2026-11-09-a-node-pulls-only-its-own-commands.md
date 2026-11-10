---
title: "A node's command pull reads only its own components' commands (#1631)"
date: 2026-11-09
refs: [1631, 1684, 1630, 1626, 1623]
---

Every node asks for its pending commands once a heartbeat, and the pull used to read the whole
fleet's deliverable commands, choose an actuating endpoint for each, and only then keep the ones
placed on the asking node: each node paid for every node's backlog. `PendingNodeCommands` now starts
from the components that carry an attached endpoint on the asking node and joins their commands, so
the rows it reads are found by the node's own components. The endpoint choice still ranges over all
of a component's endpoints, so a component with endpoints on two nodes is still delivered by exactly
one of them.

A new migration adds `command_deliverable_idx` on `command (component_id, ts)` where the command is
owned by a component and not yet executed, the access path the scoped pull reaches. The older command
indexes are keyed for settlement, and `now()` is not immutable, so the delivery window stays a range on
the `ts` key column rather than an index predicate. The ticket's example predicate also carried
`status = 'issued'`; that term is left out until #1630 makes a terminal command leave the deliverable
set by status, and whichever of #1630 and #1626 (which plans its own `(ts) where executed_at is null`
index) lands next reconciles the command indexes to one.

The dispatched stamp is now one `update ... from unnest(...)` statement for the whole pull instead of
one update per delivered command, so a pull delivering twenty commands issues as many statements as one
delivering one.

Proof: `TestCommandPullReachesTheDeliverableIndex` explains the statement the gateway really issued
and holds its command scan to `command_deliverable_idx` searched by `component_id`;
`TestCommandPullStatementsDoNotGrowWithItsDeliveries` counts the statements of a 1 and a 20 command
pull; `TestCommandWireDelivery` issues commands on two nodes' components and on a component with
endpoints on both, and proves each node's pull returns only its own and leaves the other's undispatched;
`TestCommandDeliverableIndexMigrationRoundTrips` rolls the migration back and reapplies it.

Thin cut: the ticket's command retention (a `retention.command_days` setting, `PruneCommands` and a
command lane in the retention pass) moved to #1684, so the command table is still never pruned.
