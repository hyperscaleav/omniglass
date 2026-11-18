---
title: "A node refused its first worklist heartbeats and reports its pull loss (#1882)"
date: 2026-11-16
refs: [1882]
---

A node whose first worklist pull is refused no longer goes silent. It now builds its node-self report
before that pull, runs the pull on a goroutine of its own, and on every heartbeat tick meanwhile
publishes its heartbeat and its node-self account; each first pull counts toward
`worklist-pull-loss-ratio` as every later pull does (#1868). A node refused from the start reads up
with a pull loss of 1 rather than down, so the health page and the lab's `worklist_loss` gate name
the real cause ([ADR-1882](/architecture/decisions/#adr-1882-a-node-heartbeats-and-counts-its-pulls-before-its-first-worklist)).
`TestANodeRefusedItsFirstWorklistReportsItsPullLoss` failed before the change (the node never
heartbeat) and passes after, with the first pull's tally and the tick-while-waiting covered by unit
tests.
