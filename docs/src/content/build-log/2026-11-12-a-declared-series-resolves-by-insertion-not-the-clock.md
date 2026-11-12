---
title: "A declared or intended series resolves by insertion, not the wall clock (#712)"
date: 2026-11-12
refs: [712, 1206]
---

`TestDeclaredValuesAppendToSeries` and `TestLocationPropertiesAPI` each failed once in a run of
hundreds, with a tombstone losing to the row before it. The cause was never a tie: the series ordered
by `ts desc, id desc`, and `ts` is the database's wall clock, so a clock stepping backwards between
two writes to one series made the earlier row the current one.

A declared row never carries a caller's timestamp, and nor does an intended one (the database stamps
it at the command's open), so for those two lanes the identity id is the authoritative order and `ts`
only an annotation. Both now resolve by `id desc` in every place that reads their current row:
`LatestValue`, the set's and the clear's append-only-on-change guard, `currentDeclared`, the
effective-property read, the told value and the per-command intended reads, the settlement reads and
the metric lane's intended tier. The new `seriesOrderSQL` names the rule for the one query that takes
the provenance as an argument. The observed lane keeps `ts desc, id desc`,
because a node or device supplies its `ts`, and each `ts`-ordered read on the audit list (the calc
inputs and window, the observed-per-instance walk, the endpoint rejection and key pin) now carries a comment saying which order it uses and why.

Three tests on a real Postgres fix it in place: a declared series whose second row carries an earlier
`ts` resolves to the second row through the latest read, the effective read and the write guard; an
intended series with a later `ts` on the first command resolves to the later command through the latest
read and the told value; and an observed series with a backfilled older sample written last still
resolves to the newer reading.
