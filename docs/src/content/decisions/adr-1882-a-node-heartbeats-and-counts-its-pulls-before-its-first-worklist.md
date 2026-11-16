---
title: "A node heartbeats and counts its pulls before its first worklist"
date: 2026-11-16
status: "Accepted"
summary: "Amends ADR-1577's first-pull rule. A node heartbeats and sends its node-self account on every heartbeat tick from start, while its first worklist pull is still retrying, and every first pull, served or refused, counts toward worklist-pull-loss-ratio. A node refused from the start reads up with a pull loss of 1, not down"
refs: [1882, 1577, 1868]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [platform health](/guides/operator/platform-health/)
- **Decision:** (#1882, amending the first-pull rule of
  [ADR-1577](#adr-1577-a-node-worklist-is-compressed-served-by-a-bounded-responder-and-retried).)
  The node builds its node-self report before its first pull and runs that pull on a goroutine of
  its own. Until the first worklist lands, each heartbeat tick publishes the heartbeat, retries the
  spool and sends the node-self account; each first pull is tallied on the report as served or
  refused, as every later pull is (#1868). A pull stopped by shutdown is not tallied.
- **Context:** ADR-1577 held the heartbeat until the first worklist, so a node still waiting read as
  not up. When the server refuses that pull (a reply over the bus's limit, or a busy server), the node
  reported nothing at all: no heartbeat and no `worklist-pull-loss-ratio`, so the health page and the
  scale lab's `worklist_loss` gate read it as a silent node, and the loss named the wrong cause.
- **Consequences:** A node refused from the start heartbeats, its platform component carries its
  node-self figures, and it reads a pull loss of 1 every window until a worklist lands; the seeded
  `worklist-stale` rule fires on it like any other refused node. A node up with no worklist yet
  is no longer told apart by its heartbeat; its pull loss and its worklist task count, 0 until a
  worklist lands, say it. The server's report window rides the worklist, so until then a node
  started without `--report-every` reports on the default window of one minute. Its lane count
  and a busy fraction of 0 are reported as for any idle node.
- **Not chosen:**
  - **Keep the silence and read it as down.** It is the defect: down and refused have different
    fixes, and only the node knows which it is.
  - **A metric of its own for the first pull.** Its reader would be the same gate and the same
    column as the running node's pull loss, so a second key adds a branch with the same body.
