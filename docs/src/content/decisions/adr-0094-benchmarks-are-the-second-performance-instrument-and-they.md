---
title: "Benchmarks are the second performance instrument, and they gate nothing"
date: 2026-08-09
status: "Accepted"
summary: "Performance has two instruments: round-trip counting gates in `make test`, wall-clock benchmarks (`make bench`, two estate sizes, fixtures outside the timed section) are diagnostic and gate nothing; no CI perf job, no stored baseline, no `EXPLAIN` assertions (deferred), no timing assertion anywhere, and one candidate benchmark was measured as three-quarters transport and dropped rather than shipped. **Amended (#725):** the `EXPLAIN` deferral holds for the plan a planner PREFERS and is lifted for the access path a query can REACH; planned under `enable_seqscan = off` that answer does not move with the fixture's size, so an access-path assertion on one relation (index name AND index condition, never plan shape, never a duration) becomes a third instrument that gates"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [test-driven](/contributing/test-driven/)
- **Decision:** Performance has two instruments, and only one of them gates. **Round-trip counting**
  (`internal/storage/storagetest/querycount`, asserted in `list_cost_test.go`) stays the deterministic
  net that runs in `make test` and blocks a merge. **Wall-clock benchmarks** (`make bench`, ten
  benchmarks over the real Gateway at two estate sizes) are diagnostic only: run deliberately before
  and after a change that claims a performance effect, and compared with `benchstat`. Counting catches
  the N+1 exactly; benchmarks catch what counting is blind to, which is everything inside a single
  statement (a dropped index, a plan flip to a sequential scan, a recursive CTE that stops being
  bounded, a predicate that stops being sargable). Every benchmark runs at a small estate and a larger
  one, because one size cannot tell a constant apart from a linear cost, and every fixture is built
  outside the timed section, because a benchmark that provisions inside its own loop measures the
  harness.
- **Not chosen, and why each was refused rather than deferred by accident:**
  - **No CI job and no merge gate on a duration.** A wall-clock threshold on a shared runner either
    sits so loose it catches nothing or it flakes and gets muted. A perf job everybody ignores is
    worse than none, because it reads as coverage. A Go benchmark is inert without `-bench`, so this
    is the natural shape rather than a compromise.
  - **No stored baseline artifact.** `benchstat` comparison needs a baseline per commit or per
    release plus a policy for when a regression blocks a merge, which is real infrastructure that
    should be built because it is needed, not because benchmarks felt like diligence. A number
    committed to the tree is a number from a different machine on a different day.
  - **No `EXPLAIN` plan assertions,** deliberately deferred rather than dropped. They are the sharpest
    tool for the dropped-index class and cost no timing at all, but a plan depends on table
    statistics, so a fixture too small plans differently from production and the assertion pins the
    wrong shape. Revisit when a fixture is realistic enough for the planner to agree with production,
    or when a specific plan regression proves the benchmarks insufficient.

    **Amended ([#725](https://github.com/hyperscaleav/omniglass/issues/725)):** the deferral stands for
    the plan a planner **prefers** and is lifted for an access path a query can **reach**, which is a
    different question with a different dependence on statistics. Neither revisit condition above is
    met (no fixture here is production-sized, and nothing regressed a plan) and neither is claimed:
    what ships is narrower than what was deferred. Planned with `set enable_seqscan = off`, which
    prices the sequential path out rather than forbidding it, `EXPLAIN` answers "which index is this
    relation reachable by", and that answer does not move with the fixture's size: measured over the
    health reads on an empty database, on a 45-row fixture with no statistics at all, and on the same
    fixture analyzed, the join shapes around the scan differed in all three (hash join to merge join, a
    sort appearing and disappearing) while the scan of `property` was the same index scan carrying the
    same index condition in every one. The instrument is
    `internal/storage/storagetest/accesspath`, it gates in `make test` as counting does, and three
    rules bound it. Assert the access path of **one relation**, never the plan's shape. Assert the
    **index condition** as well as the index name, because a coerced predicate or a leading column
    dropped from the filter leaves the index NAMED in the plan and walked rather than searched, which
    reads as a pass. And never assert a duration or a preference, which is this entry's standing
    invariant, untouched. It earns its place on the class no other instrument here can see: a partial
    index can sit in `pg_indexes` and be unreachable by the read it was built for, because its
    predicate stopped being provable from the query's own clauses, and the statement count is identical
    either way.
  - **No timing assertion anywhere,** in a benchmark or a test. This is a standing invariant, not a
    property of this slice.
- **What the first run measured, because a benchmark set is only as honest as its floor.** One pool
  acquire and one empty statement costs about 265us on the dev box, and every other number contains
  one copy of that per statement the call issues. The reads land between 0.7ms and 5.7ms with a
  run-to-run spread near 5%, so a plan regression in them is visible. The health RECOMPUTE chain does
  not: a raise-and-clear pair costs about 22ms across 58 statements, so roughly three quarters of it
  is transport that no planner decision can move, and its spread is three times the reads'. It was
  built, measured, and deliberately not shipped, because a benchmark that cannot detect the regression
  it appears to watch is worse than none. A path that round-trip-bound is counted, not timed, which is
  the same reasoning that made counting the first instrument.
- **Context:** [#651](https://github.com/hyperscaleav/omniglass/issues/651), filed out of Fred's
  question during [#643](https://github.com/hyperscaleav/omniglass/issues/643): "how do we know if
  performance decreased or increased". [#650](https://github.com/hyperscaleav/omniglass/issues/650)
  answered the counting half. This is the half counting cannot answer, and it was deliberately
  sequenced after [#649](https://github.com/hyperscaleav/omniglass/issues/649) replaced per-test
  migration with a template-database copy: while provisioning was about 90% of a storage run, timing
  anything on top of it measured the harness.
