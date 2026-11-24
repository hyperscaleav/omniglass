---
title: "Omnibot's tool calls fold into a collapsed thought block (#2422)"
date: 2026-11-23
refs: [2422, 2303, 2261, 2260]
---

The panel used to show each tool call as a full card, so a short answer sat under a wall of detail.
The reasoning and tool calls of a turn now fold into one collapsed block, in order, with the answer
below it. Collapsed it is one line: what is happening while the run is live ("Running
list-components…", "Waiting for you on …"), then "Thought · 3 tool calls" once settled, with an
error icon and "· 1 failed" if a call failed. Expanded, each call is a slim row (tool, a short
argument summary, state) that opens to its full input and result; nothing is open by default.

Grouping, the summary line and the failure test are pure functions in `web/src/lib/assistant.ts`
(`groupThoughts`, `thoughtSummary`, `stepFailed`), so a run resumed after an ask groups the way a
live one does. The AG-UI result event carries no error flag, so a call counts as failed when it was
declined, its result is plain text rather than JSON, or the JSON has an `error` key. The block shows
no elapsed time yet ("Thought for 6s"); that needs timestamps on the events.
