---
title: "Settlement reads one clock, and a zero window stops arguing with it"
date: 2026-08-12
refs: []
form: bullet
---

- **Settlement reads one clock, and a zero window stops arguing with it.** `Settle` compared a
  sample's `ts`, written by Postgres, against a `now` supplied by the Go process, so the verdict was a
  function of the skew between two hosts. At a zero settle window the comparison reduces to "is this
  sample stamped in the future", with no margin at all to absorb the difference, which is how the
  setting whose whole meaning is "do not wait" became the one that could wait forever: a command that
  genuinely failed reported `pending` and never settled. It showed up first as an intermittent
  end-to-end failure and was reproduced on a branch containing no Go at all. Both settle paths now read
  `now` from the database in their own transaction, which at issue is the very timestamp the intended
  row was stamped with, and a zero window is terminal before any arithmetic runs. `Settle` stays pure
  and still takes `now`; what changed is who supplies it, at the cost of one round trip on each of the
  two paths. The alternative, stamping samples from Go, was refused as the larger ripple, and a
  tolerance was refused as the move that quiets a test without changing the behavior
  ([ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)).
