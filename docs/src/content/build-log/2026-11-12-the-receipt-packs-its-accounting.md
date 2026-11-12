---
title: "The receipt packs its accounting (#1699)"
date: 2026-11-12
refs: [1699, 1641]
---

A telemetry receipt's ledger and outcome are integer arrays now, not jsonb objects: the ledger a
(count, before) pair per kind slot, the outcome sparse (code, n) pairs under codes that never move. The
receipt is one row a batch, so the names a jsonb object repeated on every row were a fixed cost on every
batch. A migration converts the stored rows in place and back. On a three-system corporate lab, receipts
a day fell from 219 to 151 MiB, about 942 to 669 B a batch, with loss 0 on every kind and hop. The
scaling page's receipts-a-day target moves from 0.5 to 1.2 GiB at 1,000 systems
([ADR-1699](/architecture/decisions/#adr-1699-the-receipt-packs-its-accounting-and-its-target-is-1-2-gib-a-day)):
the old figure predates the per-batch accounting, and no receipt carrying it fits under 0.5.
