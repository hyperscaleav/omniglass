---
title: "Lists that promise write order order by a DB-assigned sequence"
date: 2026-08-21
status: "Accepted"
summary: "A read that promises write order orders by a bigint identity the database assigns at insert, never by `created_at` or a uuidv7 id: wall-clock steps (reproduced on WSL2 under load) invert clock-derived keys across transactions; audit and principals converted, the rest tracked by #801"
---

- **Date:** 2026-08-21 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** A read whose contract is "in the order it was written" orders by a
  `bigint generated always as identity` column the database assigns at insert, never by
  `created_at` (or any `now()` stamp) and never by a uuidv7 id. `audit_log` and
  `principal` gain the column (`db/migrations/20260821220000_write_sequence.sql`); the
  audit trail orders by `seq desc` and the principal directory by `kind, seq`. Remaining
  clock-ordered reads (the health ledger's id-ordered history, ts-ordered event, log,
  and sample reads) are tracked by #801 and convert the same way when touched.
- **Context:** The #780 screenshot-gate flaps (the audit page interleave, the users
  directory row swap) resisted three ordering fixes because every candidate key was
  secretly the same key: `now()` and uuidv7 both read the wall clock, and on a loaded
  WSL2 host the clock is not monotonic; NTP correction steps it several seconds mid-run,
  so stamps taken by different transactions invert relative to true write order. A
  six-run probe caught a fixture user stamped 5.6 seconds after a row its own process
  wrote later. An identity column is allocated by the database in insert order and no
  clock touches it; regression tests simulate the step by rewriting committed rows'
  clock keys and asserting the lists hold.
