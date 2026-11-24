---
title: "The view seam no longer double-delivers a row (#2357)"
date: 2026-11-23
refs: [2357, 2276, 2056]
---

`TestReadViewsSeamHasNoGapOrDuplicate` failed once under load with a sample seen in both a
snapshot's history and the tail after its cursor. The cause was a real seam duplicate, not a test
race: `Hub.Commit` makes the rows durable in the database and only then publishes them to the ring,
and `Mark` compared only the sequence number. A snapshot opened after the database commit but
before the publish held the row in its history, yet `Mark` returned a cursor before it, so the
row arrived again on the tail.

The hub now counts commits begun and in flight. `Mark` accepts an attempt only when no commit was
in flight as it began and none began during the snapshot; otherwise it retries (and returns the
retryable busy error after the existing retry budget). Regression test:
`TestMarkWaitsOutACommitDurableButNotPublished` in `internal/tail`.
