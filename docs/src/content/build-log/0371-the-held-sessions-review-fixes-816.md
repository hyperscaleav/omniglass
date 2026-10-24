---
title: "The held session's review fixes (#816)"
date: 2026-10-23
refs: [816]
---

The slice's review, fixed before the rollup, each with a regression test that failed first. A
timed-out exchange recycles its connection (`ErrExchangeTimeout`), since its late reply would answer
the next ask forever (`TestHeldSessionTimeoutNeverAnswersTheNextAsk`: SLOW GET INPUT times out, GET
VOLUME gets VOLUME); the held line connection was rebuilt so one reader goroutine draws every ask's
boundary (discard what already arrived, write, hand the next line over), so a banner sent on connect
is never an answer (`a banner on connect is never an answer`, fifty fresh connections). An attempt
now fails on a churned connection or an answer timeout as well as a failed dial, so an
accept-then-drop or a hung device reads down within the attempt budget instead of up forever
(`TestSessionAcceptThenDropReadsDownWithinTheBudget`, `TestHeldSessionToAHungDeviceReadsDown`), and a
stable connection's drop still never does. The first dial's wait window closes inside the up
transition, and a listen task's spec re-hash keeps a healthy connection while a credential change
recycles it (`TestSessionManagerKeepsTheConnectionAcrossATaskRehash`). ADR-0150 now says the last
`session-state` outlives a teardown and is read by its age.
