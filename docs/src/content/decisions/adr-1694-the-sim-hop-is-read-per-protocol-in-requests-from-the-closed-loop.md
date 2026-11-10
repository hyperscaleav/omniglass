---
title: "The sim hop is read per protocol, in requests, from the closed loop"
date: 2026-11-10
status: "Accepted"
summary: "Accepted: `lab measure` reads the delivery table's sim hop from the closed loop's marks, one row per protocol the nodes tally, counted in requests rather than per kind in items. In is every request the simulators settled on tallied traffic, out the replies that reached the node; a refusal or error that came back is returned, and the loss splits into withheld, dropped and not-returned. End to end composes one loss pooled over every protocol into each kind. Rendering the batch-count gaps `published-not-landed` and `spool-dropped` as ratios is deferred to #1738."
refs: [1694, 1641, 1639, 1623, 1692, 1693, 1738]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [lab guide](/guides/lab/#reading-the-delivery-table), [messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio)
- **Decision:** (#1694, under #1623.)
  1. **Per protocol, in requests.** The issue asked for a sim row per kind, items the endpoints
     answered against items the node collected. A simulator answers requests and cannot know which
     kinds an answer would have carried, so the sim hop is one row per protocol the nodes tally
     (`closedloop.SimHop`), counted in requests. The JSON row carries `protocol` in place of `kind`.
  2. **Read from the closed loop's narrowest span.** Each node's in is the requests the simulators
     settled between the start mark's second simulator read and the end mark's first, less the
     traffic the node does not tally; lost is what did not come back, less what is still in flight at
     the node's end read. A node that restarted adds nothing to the summed rows, as it reads no gaps
     ([ADR-1639](/architecture/decisions/#adr-1639-the-scale-lab-closes-the-loop-from-a-pull-only-bracketed-snapshot)),
     and the table names it (`sim not counted`) so a row never passes for every node's.
  3. **A refusal or error that came back is returned.** The device said no and the task reports it;
     the hop carried the reply. Loss is a reply that never reached the node, by cause: `withheld`,
     `dropped`, then `not-returned` for the rest. Dedup reads `-`, since a request is never
     redelivered.
  4. **End to end pools the sim hop.** The sim rows' requests are summed into one loss and composed,
     `1 - prod(1 - loss)`, into every kind with an ingest row. This approximates: a lost request
     carried some kinds and not others.
  5. **The table outlives a missing ingest read.** When receipts cannot be read, the sim rows still
     print and the table ends with `ingest not measured` and the reason. The converse holds too: a
     closed loop that was not read prints `sim not measured` under the ingest rows, since end to end
     then covers the ingest hop alone.
- **Context:** #1641 defined one loss ratio per kind per hop and read only the ingest hop. #1639's
  marks already bracket the simulators and the node's request tally, so the sim hop's counts were
  there to read; this slice reads them as a ratio beside the ingest rows.
- **Thin cuts:** #1639's `published-not-landed` and `spool-dropped` still print as batch-count gaps
  in the endpoints block rather than as loss ratios in the table (#1738, which leans on the node hop,
  #1692).
- **Not chosen:** apportioning a lost request to kinds by the task's datapoints. The node knows which
  task a request served, the simulator does not, and joining them per request needs a correlation
  neither side carries today.
