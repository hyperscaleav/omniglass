---
title: "A transition never waits on a rule being deleted behind a send (#1335)"
date: 2026-11-14
refs: [1335, 618, 1321]
---

Deleting an action rule while the notification tick held one of its rows mid-send left the
`DELETE` waiting on that row for its `SET NULL`, and a transition the rule matched then queued a
notification whose foreign key check waited behind the delete, for up to a send timeout. The
queue now reads the rule, type and target rows `FOR KEY SHARE SKIP LOCKED` before it writes, so
a rule mid-delete is skipped for that transition (the outcome it had, minus the wait) and every
other rule still queues. A storage test holds a notification row in a raw transaction, blocks a
rule delete behind it, and raises an alarm the rule matches: the raise returns at once with only
the other rule's row, and the delete finishes when the send ends. The notifications section of
the alarms and actions page states the rule.
