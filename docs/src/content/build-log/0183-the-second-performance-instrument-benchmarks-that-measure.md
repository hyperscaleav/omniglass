---
title: "The second performance instrument: benchmarks that measure, and gate nothing"
date: 2026-08-09
refs: []
form: bullet
---

- **The second performance instrument: benchmarks that measure, and gate nothing**
  ([#651](https://github.com/hyperscaleav/omniglass/issues/651), [ADR-0094](/architecture/decisions/)).
  Counting round trips catches the N+1 exactly and deterministically, and is blind to everything that
  happens inside one statement: a dropped index, a plan flip to a sequential scan, a recursive CTE that
  stops being bounded. `make bench` is the instrument for that blind spot. Ten benchmarks run over the
  real Gateway against real Postgres, at a small estate and a larger one, and none of them runs in
  `make test`, gates a merge, or asserts a duration. A Go benchmark is inert without `-bench`, so
  diagnostic is the natural shape rather than a compromise, and the whole point of the previous slice
  was that a wall-clock threshold on a laptop either catches nothing or flakes until it is muted.

  The sequencing mattered. While per-test migration made provisioning about 90% of a storage run,
  timing anything on top of it would have measured the harness, which is why this waited for the
  template-database copy. The same trap shows up one level down, and the fixtures are shaped to avoid
  it: each estate is built once for the whole binary and shared, never inside a timed loop, and the
  timer resets after the fixture is in hand. The two sizes are not decoration either. One size cannot
  tell a constant apart from a linear cost, which is the entire question: a list should grow with the
  estate, a tag cascade walking one component's ancestors should not, and only the pair says whether
  that is still true. It is: `ResolveTags` reads flat across a tenfold estate while `ListComponents`
  and the batch `EffectiveTags` grow with it.

  One benchmark was built, measured, and deliberately not shipped, and the reasoning is the useful
  part. The health recompute chain (a raise and a clear over a staffed component) costs about 22ms and
  issues 58 statements to do it. Against a measured round-trip floor of about 265us, roughly three
  quarters of that number is transport no query planner can move, and its run-to-run spread is three
  times the reads'. A regression that halved every plan in it would move the total by less than its own
  noise. That benchmark could not fail for the reason it appeared to exist, so it is a comment in
  `bench_test.go` naming the gap instead of a number in the output implying coverage. A path that
  round-trip-bound is counted, not timed.
