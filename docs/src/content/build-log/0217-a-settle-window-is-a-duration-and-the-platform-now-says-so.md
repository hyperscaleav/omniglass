---
title: "A settle window is a duration, and the platform now says so"
date: 2026-08-13
refs: []
form: bullet
---

- **A settle window is a duration, and the platform now says so.** `settle_window_seconds` had a
  default of 0 in four places and a floor in none, so a negative one was accepted by the column, the
  API body, the console and the seed loader alike. It is not a shorter wait: `Settle` tests
  `windowSeconds > 0`, so -5 behaves exactly as 0 does, which made it a value an operator could set,
  read back on the row, and never see honoured on any command of that type. The schema now floors the
  field at 0, so the generated client and CLI carry the floor too, and the gateway refuses one on
  create, on update and on the boot-seed upsert, the last naming the row the way its target refusals
  already do. Zero stays legal and unchanged: it is the documented way to say "settle immediately"
  ([ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)),
  and it is what the shipped `reboot` carries. In the same pass, the settle-check's comment claiming
  its `now()` is strictly later than the intended row's `ts` was corrected: READ COMMITTED does not
  provide that ordering, and while no verdict is wrong today (a negative delta is `pending`, and the
  zero case reads no timestamp at all), an invariant the isolation level does not give is one a later
  change can lean on.
