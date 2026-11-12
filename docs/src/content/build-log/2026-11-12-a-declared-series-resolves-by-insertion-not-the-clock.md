---
title: "A declared series resolves by insertion, not the wall clock (#712)"
date: 2026-11-12
refs: [712, 1206]
---

`TestDeclaredValuesAppendToSeries` and `TestLocationPropertiesAPI` each failed once in a run of
hundreds, with a tombstone losing to the row before it. The cause was never a tie: the series ordered
by `ts desc, id desc`, and `ts` is the database's wall clock, so a clock stepping backwards between
two writes to one series made the earlier row the current one.

A declared row never carries a caller's timestamp, so for the declared lane the identity id is the
authoritative order and `ts` only an annotation. The declared series now resolves by `id desc` in
every place that reads its current row: `LatestValue`, the set's and the clear's append-only-on-change
guard, `currentDeclared`, and the effective-property read. The new `seriesOrderSQL` names the rule for
the one query that takes the provenance as an argument. The observed lane keeps `ts desc, id desc`,
because a node or device supplies its `ts`, and each `ts`-ordered read on the audit list (the calc
inputs and window, the observed-per-instance walk, the endpoint rejection and key pin, the intended
reads) now carries a comment saying which order it uses and why.

Two tests on a real Postgres fix it in place: a declared series whose second row carries an earlier
`ts` resolves to the second row through the latest read, the effective read and the write guard, and
an observed series with a backfilled older sample written last still resolves to the newer reading.
The intended lane is still read by `ts`; it is stamped by the database at a command's open, so it
could take the declared rule, and that is left to its own change.
