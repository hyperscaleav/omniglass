---
title: "The Alarms page is the fleet's alarm queue (#2333)"
date: 2026-11-23
refs: [2333, 2100, 1203]
---

`/alarms` stopped being a stub. It lists every active alarm the caller can read, worst severity first, one
row each with its component, systems, location, raised-at and acknowledged and snooze state, filterable by
severity, location subtree, ack state and snooze state, with the filter kept in the address. Acknowledge,
Snooze or Unsnooze and Troubleshoot with omnibot work from the row, gated as on the component's panel, and
the queue polls at the leaf cadence and refreshes on any acknowledge or snooze.

`GET /alarms` gained the row fields `location`, `location_path` and `systems` (read in the one scoped query
and one batched path walk, not per row), the filters `severity` (repeatable), `location`, `acknowledged` and
`snoozed`, and a keyset cursor (`page_token`, `next_page_token`) on the same worst-then-newest order, so a
queue of a thousand pages rather than truncating. `omniglass alarm list` and the MCP tool take the same.
The CLI generator learned repeatable (array) query parameters to carry `--severity`.

No index was added: the existing open-alarm index serves the scoped read, and over the new `storm` devseed
scenario (the large fleet with an alarm on one in three components) the first page returns in about 18 ms,
a severity filter in about 20 ms and the unacknowledged-and-not-snoozed filter in about 290 ms, against a
2 s budget the test enforces (`TestAlarmQueueAtStormScale`).

Live is a poll: there is no alarm stream and the ingest tail does not carry rule-derived transitions.
Bulk acknowledge is deferred, and the drill to the triggering sample is #2360.
