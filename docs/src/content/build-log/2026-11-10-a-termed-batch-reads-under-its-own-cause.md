---
title: "A batch the server Terms whole reads under its own cause (#1691)"
date: 2026-11-10
refs: [1691, 1641, 1639]
---

A batch the telemetry consumer Terms whole now records a Term receipt: the batch's ledger, with every
item it declared under the cause, so the ingest loss names why it never landed instead of reading
`missing`. The five Term paths each have an outcome, all loss: `refused-owner`, `refused-task`,
`refused-batch`, `refused-metadata` and `abandoned` (the fifth failed delivery). `telemetry_receipt`
gains a `termed` column; a landing that finds a Term receipt replaces it, so a node's retry past the
duplicate window still lands its rows and reads `stored`, while a Term never overwrites a receipt.
The closed loop's receipt count (#1639) skips Term receipts. An undecodable batch has no readable
ledger, so it records nothing and stays `missing`; the issue's `refused-undecodable` outcome was not
added. The decisions are
[ADR-1691](/architecture/decisions/#adr-1691-a-batch-the-server-terms-whole-reads-under-its-own-cause-on-a-term-receipt).
