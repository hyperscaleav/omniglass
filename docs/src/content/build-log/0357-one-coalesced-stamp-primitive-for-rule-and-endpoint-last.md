---
title: "One coalesced-stamp primitive for rule and endpoint last-seen (#943)"
date: 2026-10-22
refs: [943]
---

`event_rule_live` (#929) and `endpoint_live` (#933) were built independently to the same shape: one
statement per ingest batch, stamped by the server's clock, a row stamped within 5s left alone, a row
another batch holds skipped (SKIP LOCKED), and a first sight inserted ON CONFLICT DO NOTHING. That
statement is now one primitive, `liveStamp` (`internal/storage/live_stamp.go`): a table, its key and
stamp columns, and an optional owner table whose missing rows first sight skips. The rule evaluator
calls it on its ingest transaction and the endpoint stamp on the pool, so a third live stamp is a call,
not a copy. Behavior is unchanged: the #929 and #933 tests pass unedited, and the helper's own
testcontainer tests, on a scratch table, prove concurrent first sight, the bound, the owner guard, never
waiting on a held row, and the transaction's clock.
