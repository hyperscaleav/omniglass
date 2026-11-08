---
title: "The metric roll keeps yesterday, and a retried batch logs its drop once"
date: 2026-11-08
status: Accepted
summary: "Amends ADR-1527 point 3: the roll keeps yesterday through three days ahead and refuses unless that span is covered. The test template is rolled when it is built. The too-old drop warning goes out when its batch commits, and the drop's horizon reads the database clock. Behind across a UTC midnight is a backlog, never the midnight itself, and tests pin the database clock to either side of one."
refs: [1567, 1527, 1528, 1529]
---

- **Date:** 2026-11-08 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1567, amending [ADR-1527](#adr-1527-the-metric-lane-is-partitioned-by-utc-day) point 3.)
  1. **The roll keeps yesterday.** The server keeps a partition for yesterday, today and the next
     three days, and refuses unless they cover every instant from the start of yesterday through the
     end of the third day ahead. A sample stamped a minute before midnight, landing raw a minute
     after it, finds its day with no routing miss.
  2. **The test template is rolled.** The harness migrates its template once a binary, and the
     migration makes only today through three ahead. It now runs the roll on the template before
     sealing it, so every cloned database carries yesterday too, and a test that plants a row
     straight into `metric` an hour back does not fail with 23514 when its binary built the
     template just after midnight.
  3. **A drop logs on commit.** A batch that misses a day is made its days and retried whole, and
     the too-old drop ran (and logged) in each attempt. The write now carries commit hooks: the
     drop's warning is queued and logged once, after the transaction commits, and a rolled-back
     attempt logs nothing.
  4. **The drop reads the database clock.** The drop's prefilter compared against the Go clock and
     the drop itself against the database's. It reads the database clock for both, as every other
     storage read of now does.
  5. **Behind is a backlog.** A day closes for the block fold one trend horizon after it ends, so a
     midnight closes one day and a pass that folds any day folds it. `Behind` after a midnight means
     the fold was already short, most often because its marks were first seeded at the ISO Monday of
     a week begun five or six days before.
  6. **Tests pin the database clock.** `storagetest.PinClock` shadows `now()`,
     `transaction_timestamp()`, `statement_timestamp()` and `clock_timestamp()` for one test
     database through its search path, and `storagetest.AtEachMidnight` runs a test at the real time
     and pinned 30 seconds either side of a UTC midnight.
- **Context:** main went red when the UTC day turned during a run: a raw insert into yesterday found
  no partition, a retried batch logged its drop twice, and a retention pass seeded on a Sunday read
  `Behind`. No migration was needed: the roll creates the extra day at boot.
- **Not chosen:**
  - **A migration making yesterday.** The roll runs at every boot and every retention pass; a
    migration runs once and would leave a server booted on a later day short again.
  - **Retrying a raw insert in the tests.** It would hide the routing miss rather than remove it, and
    the gateway's own retry is already tested.
  - **Faking the Go clock.** The storage layer reads the database's clock, so a Go-side fake would
    pin nothing the writes read.
