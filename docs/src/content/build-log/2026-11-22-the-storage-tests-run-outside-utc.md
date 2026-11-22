---
title: "The storage tests run outside UTC (#2313)"
date: 2026-11-22
refs: [2313, 2310]
---

`make test` failed on a host outside UTC while CI, which runs in UTC, stayed green. Two causes were
found. The metric partition roll's refusal named the first uncovered instant in the host's zone
(`2026-11-22T05:00:00-07:00`); it now names it, and the range it wanted, in UTC, as the database
and the rest of the roll do. The log-line test read the year of a scanned timestamp in the host's
zone, so a line stamped at the start of 2000 UTC read as 1999; it now reads the UTC year. The two
snooze-end tests named in the ticket did not fail under `America/Denver`, `Asia/Tokyo` or
`Pacific/Kiritimati` on this run, so no change was made to them.

The class is now guarded in the gate: the storage shard (`make test-go-storage`) runs under
`TZ=America/Denver`, so code that reads the host's zone, or a test that assumes UTC, fails in CI
and not only on a contributor's machine.
