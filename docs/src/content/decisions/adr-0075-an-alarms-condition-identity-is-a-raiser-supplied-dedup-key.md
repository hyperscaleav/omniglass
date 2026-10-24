---
title: "An alarm's condition identity is a raiser-supplied dedup key"
date: 2026-08-01
status: "Accepted"
summary: "alarm gains dedup_key and the one-open-per-condition partial unique index; RaiseAlarm becomes a guarded conditional insert"
---

- **Date:** 2026-08-01 | **Status:** Accepted | **Pages:** [workers](/architecture/workers/), [alarms and actions](/architecture/alarms-actions/)
- **Decision:** `alarm` gains a **`dedup_key`** column (the condition identity; it defaults to the message when the
  raiser supplies none) and the partial unique index `alarm_open_condition_key` on `(component_id, dedup_key)
  WHERE cleared_at IS NULL`, so **one open alarm per condition per component** is a database fact. `RaiseAlarm` is
  a guarded conditional insert: a losing raise returns the existing open incident instead of a duplicate, writing
  no audit row and recomputing nothing. The rule engine's `event_rule_id` keying joins alongside the dedup key
  with its own slice; a per-component unique index was refused (it would forbid two unrelated conditions on one
  component, contradicting the capability-degradation model).
- **Context:** workers.md reasoned from a "one-open index" that did not exist: `RaiseAlarm` was an unguarded
  insert, `alarm_active_idx` is non-unique, and the liveness sweep was idempotent only because it happens to run
  as a singleton. The table had no column naming WHICH condition was open, so the documented `(event_rule, owner)`
  key was unrepresentable before the rule table exists; the raiser-supplied key works today and does not block on
  the engine. Shipped by [#465](https://github.com/hyperscaleav/omniglass/issues/465) in the #431 loop.
