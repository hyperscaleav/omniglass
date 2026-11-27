---
title: "A lab calendar entry is a sum type callers switch on (#2798)"
date: 2026-11-27
refs: [2798, 2757]
---

`calendar.Entry` is now one typed value: a kind and exactly one payload, built by `NewEntry` (which refuses
a kind and payload that disagree), `MeetingEntry` or `ProblemEntry`, and read through `AsMeeting` and
`AsProblem`, which report false for another kind. The meetings-only `Entries`, `Now`, `Next` and `Prev`
and the `Problems`, `ProblemsNow` and `All` trio are one query that takes the kinds a caller reads
(`MeetingKinds`, `ProblemKinds`, `AllKinds`), so a new entry kind cannot nil-deref a reader. `Entry.Map()`
and `GET /lab/calendar` are byte-identical on the wire, held by a golden of a pinned day recorded before the
change, and a guard test keeps payload pointers and entry literals out of code outside `sim/calendar`.
