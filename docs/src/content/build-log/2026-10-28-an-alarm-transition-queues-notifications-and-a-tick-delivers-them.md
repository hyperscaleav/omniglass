---
title: "An alarm transition queues notifications and a tick delivers them (#1321)"
date: 2026-10-28
refs: [618, 1321]
---

The last slice of the notification lane (#618) closes the loop. An `action_rule` says when a
message is sent: on an alarm's `raised`, `cleared` or both, when a predicate holds, send a
notification type (the official one for the transition when it names none) to its targets. The
predicate is a new **action** expression site whose bindings are the alarm fact map a template
reads, in snake_case, plus the severity's rank; a test holds the two declarations together. It is
authorized as a rule (`rule:*`), its writes all-scope.

The notification is a row written in the alarm transition's own transaction, at the seam every
hand and rule raise and clear passes (`raiseAlarmTx`, `ClearAlarm`, `clearAlarmByDedupTx`),
rendered then, so a rollback leaves none, a re-raise writes none, and an acknowledgement is never a
transition. A rule whose predicate fails to run, or whose type fails to render, still writes its
rows, failed with the reason. `internal/notifier` drains the due rows every 2s, one per
transaction under `FOR UPDATE SKIP LOCKED` with the send inside the lock, backing off from 10s,
doubling, to a final failure at the 8th attempt
([ADR-0618](/architecture/decisions/#adr-0618-notifications-are-postgres-rows-drained-by-a-server-tick-not-cdc-to-a-jetstream-sender)
records the divergence from the CDC sender). Tests prove two drainers never send a row twice and a
restart loses nothing.

`GET /notifications` is scoped by the alarm's component as the alarm reads are, and
`POST /notifications/{id}:retry` (operator) re-queues a failed one. Each row joins its alarm's
timeline after the edge that queued it; the console's timeline reads it (notifying, notified,
failed to notify) and offers Retry. The admin guide gains the end-to-end task, sending an alarm to a
chat channel.
