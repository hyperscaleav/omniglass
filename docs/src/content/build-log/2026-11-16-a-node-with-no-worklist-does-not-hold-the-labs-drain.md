---
title: "A node with no worklist does not hold the lab's drain (#2008)"
date: 2026-11-16
refs: [2008]
---

The scale lab's drain no longer waits on a node whose latest `worklist-task-count` is 0. Such a node,
refused its first worklist after a restart, heartbeats and reports a count of 0 while it waits
(ADR-1882), but runs no task, so the drain held to its three-minute bound for a collected sample it
could never land, and `omniglass lab measure` failed as not drained. It now passes the node over and
the measure fails on `worklist_loss/<node>`, the real cause
([ADR-2008](/architecture/decisions/#adr-2008-a-node-with-no-worklist-does-not-hold-the-labs-drain)).
`TestIntegrationDrainedQueryPassesOverANodeWithNoWorklist` failed before the change (not drained
while the waiting node held it) and passes after, including the node being waited for again once it
installs a worklist.
