---
title: "Notifications are Postgres rows drained by a server tick, not CDC to a JetStream sender"
date: 2026-10-28
status: Accepted
summary: "An alarm transition writes its notification rows in its own transaction (`notification`, rendered then, `queued`), and a 2s server tick delivers them one row per transaction under `FOR UPDATE SKIP LOCKED`, the send made while the row is locked: backoff from 10s doubling, `failed` at the 8th attempt, `:retry` re-queues. Replicas share the queue and never send one row twice outside the crash window between a send and its commit; a restart loses nothing. Diverges from the page's CDC-to-JetStream action sender (#430, unbuilt), as ADR-0148 did for calc rules"
refs: [618, 1321, 430]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [workers](/architecture/workers/), [messaging](/architecture/messaging/)
- **Decision:** The notification lane's delivery engine (#618, decision 2, approved as recommended):
  1. **Rows in the transition.** `raiseAlarmTx`, `ClearAlarm` and `clearAlarmByDedupTx`, the one seam
     every hand and rule transition passes, read the enabled action rules on that transition, run each
     rule's `when` over the alarm facts, render its notification type, and insert one `notification`
     row per enabled target in the transition's own transaction. A rollback leaves no row; a re-raise
     that lands on an open alarm writes none; an acknowledgement is no transition. A `when` that fails
     to run or a type that fails to render still writes its rows, `failed` with the reason, so the
     problem is on the alarm's timeline; the transition never fails for a notification.
  2. **A server tick drains them** (`internal/notifier`, every 2s, up to 100 rows a pass, a 60s
     deadline, cancelled on shutdown, a panic recovered and logged). Each row is its own transaction:
     the oldest due row `FOR UPDATE SKIP LOCKED`, its target's credential unsealed, the send made
     through the egress primitive while the lock is held, the outcome recorded, the commit. Two
     replicas draining at once skip each other's rows, so a row is sent twice only if a replica dies
     after the send and before its commit; the `Idempotency-Key` (the row's uuid, the same on every
     attempt) is what lets a receiver drop that one.
  3. **Retry policy in code:** a failed attempt waits 10s, doubling after each (10s, 20s, ... 640s),
     and the 8th failure is final, about 21 minutes from the first attempt; a deleted or disabled
     target, or a secret that no longer unseals, fails the row at once. `POST /notifications/{id}:retry`
     re-queues a failed row for a fresh run of attempts, the same bytes and the same key.
- **Context:** The alarms-and-actions page designs the action sender as a JetStream consumer fed by
  the CDC publisher, explicitly not a Postgres `SKIP LOCKED` relay. The CDC publisher (#430) is
  unbuilt, and the notification must commit with the alarm that caused it, which a publish after the
  commit cannot promise without the outbox that CDC would be. ADR-0148 chose this same shape for calc
  rules, and the event rule sweep runs on a tick too (ADR-0144 point 6). The row does not change when
  the drain moves: once #430 lands, the CDC publisher can fan the committed rows onto JetStream and a
  consumer can take the tick's place. Holding a row lock across a send of at most 15s (the egress
  bound) costs a connection per send in flight, which one serial pass per replica bounds.
