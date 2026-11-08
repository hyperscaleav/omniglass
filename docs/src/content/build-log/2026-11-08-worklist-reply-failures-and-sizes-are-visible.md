---
title: "Worklist reply failures and sizes are visible (#1579)"
date: 2026-11-08
refs: [1579, 1577, 1571]
---

The server's worklist handler dropped every error, so a reply over NATS's `max_payload` looked to the node
exactly like a timeout. A failed read, a marshal error and a failed `Respond` now each log once at WARN,
naming the node, the subject, the error and the reply size; a reply over the limit logs at ERROR with its
size and the limit and says the node will see a timeout. Every sent reply's size is recorded per node
(`Server.WorklistReplyBytes`, the server has no metrics endpoint yet) and logged at DEBUG, and a reply past
75% of the limit warns once per node per hour.

`max_payload` is now set explicitly: `bus.DefaultMaxPayload`, 1 MB, the same value the server defaulted to,
overridable per `bus.Config`. The measurement the delivery design in #1577 rests on is
`TestWorklistReplySizeMeasure` (`WORKLIST_MEASURE=100,1000`): HQ's worklist is 929,057 bytes at 100 systems
(89% of the limit) and 3,273,096 bytes at 1,000 systems (4,434 tasks, 312% of it), so at that scale HQ's node
cannot pull its worklist at all. How worklists are requested and delivered is unchanged; that is #1577.
