---
title: "A node with no worklist does not hold the lab's drain"
date: 2026-11-16
status: "Accepted"
summary: "Amends ADR-1896's drain rule. A live node whose latest worklist-task-count is 0 runs no task and so lands no collected sample, and the lab's drain no longer waits for one from it; the measure goes on to read its worklist pull loss and fails worklist_loss naming it. A node that never reported a count is still waited for"
refs: [2008, 1896, 1882]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [lab](/guides/lab/)
- **Decision:** (#2008, amending point 5 of
  [ADR-1896](#adr-1896-ingest-lands-on-a-lane-per-partition-of-the-nodes-name-in-order-per-node).)
  A node counts as live for the drain only while its latest `worklist-task-count`, on its platform
  component, is not 0. A node with no count at all (no platform component, or no report yet) is
  still waited for, as before.
- **Context:** Since [ADR-1882](#adr-1882-a-node-heartbeats-and-counts-its-pulls-before-its-first-worklist)
  a node refused its first worklist heartbeats while it waits and reports a task count of 0. A node
  that collected in an earlier run and restarts into that state met both of the drain's live tests
  (a recent heartbeat, a series it once collected) but runs no task, and its node-self samples land
  with no collector. The drain waited its full bound for a sample that could not come, and the
  measure failed as not drained instead of on `worklist_loss`, which names the cause.
- **Consequences:** The measure reaches the `worklist_loss` gate and fails it as
  `worklist_loss/<node>` at 1. A node with a worklist that holds no task (nothing assigned to it) is
  passed over too, rightly: it collects nothing. The count rides the node's tick, so a node that
  installs its worklist just before the end mark is waited for once that count lands, and until then
  any of its batches still in flight can read as `published-not-landed` on that node alone.
- **Not chosen:**
  - **Pass over a node whose latest pull loss is 1.** A node refused only its later pulls keeps
    running the worklist it holds and still lands samples, so a full pull loss does not say it
    collects nothing; its task count does.
  - **Wait for its node-self sample after the mark instead.** That proves its lane drained but holds
    the drain up to a report window (a minute by default before a worklist sets one) for a node whose
    only figure of interest is its pull loss.
