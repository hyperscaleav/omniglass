---
title: Test-driven, always
description: Build the failing test before the feature; each change carries the tier that proves it.
---

The loop, in order, for every behavior change:

1. **Define the behavior.** State what the feature does and how it is observed, as an
   assertion, not a vibe.
2. **Write the failing test.** It must fail for the right reason before any machinery
   exists. A bug fix starts with a test that reproduces the bug.
3. **Build the minimal machinery** to make the test pass. Nothing more.
4. **Refactor** with the test green.

A change that adds or alters behavior is incomplete without a test that failed before it
and passes after. Each change carries the right tier(s): **unit** for logic,
**integration** (real Postgres) for anything touching storage, **e2e** (API, CLI, UI) for
user-facing behavior. Bug fixes start with a failing regression test that stays in the
suite. `make test` is the gate: green before commit and before merge. Validate locally;
do not lean on CI to find what a local run would.

## The spike carve-out

A spike to learn whether something is *possible* may precede tests, but it must be labeled
a spike and either deleted or stabilized with tests before it merges. "Spike" is not a
standing excuse to skip the failing test.

## The capability-primitive carve-out

When a unit wraps an environment-risky capability (raw sockets, ICMP, privileged
syscalls, an external protocol), a fake-based unit test is necessary but not sufficient.

- Commits may be incremental: a fake-green seam is a legitimate checkpoint commit.
- The real-implementation integration test is required to **close the increment** and is
  an absolute gate before any merge. It is never dropped, only sequenced within the
  increment.

The environment risk is the point of the primitive. A green fake with the real path
unproven proves nothing about the capability.

## Tiers

- **Unit:** pure logic, fast, no I/O. Expression compile/eval, decode, request shaping,
  mapping.
- **Integration:** real Postgres, no mocking the database. `testcontainers-go` gives each
  run an ephemeral instance on a random port; never bind a fixed host port.
- **End-to-end:** emulate the user at each entry point against the running stack: API
  (drive the contracts as a client), CLI (run the real commands), UI (browser-drive the
  SPA). Assert the user-observable outcome, not internals.

No mocking the system under test. No tests-within-tests.

## The storage test harness

Integration and end-to-end tests share one real-Postgres harness,
`internal/storage/storagetest`. It starts a single container per test binary,
lazily, and hands each test a fresh, migrated, isolated database, so tests never
share mutable state or collide on a host port.

The migration chain is applied **once per test binary**, into a template
database, and each test's database is a `CREATE DATABASE ... TEMPLATE` copy of
it. A copy is a file-level clone rather than a replay of every migration, and it
carries `schema_migrations` with it, so a provisioned database is
indistinguishable from a migrated one, including to dbmate. Isolation is
unchanged: every test still gets its own database.

Starting the container and building the template are both per-binary work, so
the harness caches **success and never failure** on each. A `sync.Once` here
remembers one transient hiccup (a flapping container, a refused admin
connection) and fails every remaining test in the binary from it, all at 0.00s
with the same replayed message, which buries the one failure that was real.
Retrying on the next call keeps a blip costing one test, which is what the
per-test migration replay used to give for free. A start that fails terminates
whatever container it was handed on the way out: testcontainers returns the
container alongside the error when the create succeeded and the wait did not, so
a retry over a caller that drops it leaks one Postgres per attempt.

Postgres refuses to copy a template that has live connections, and
`internal/migrate` builds a `dbmate.DB` it never explicitly closes, so the
harness does not assume dbmate left nothing behind. After migrating it
terminates any backend still attached to the template and then sets
`allow_connections = false`, which makes the copy deterministic rather than
timing-dependent. Retrying on `source database ... is being accessed by other
users` would only have turned a deterministic failure into an intermittent one.

Cleanup is a hard contract, not a convenience. Every package that uses the
harness **must** route its tests through `storagetest.Main` from a `TestMain`:

```go
func TestMain(m *testing.M) { os.Exit(storagetest.Main(m)) }
```

`Main` terminates the shared container after `m.Run()`, in-process, on normal
exit. This is the reason cleanup is reliable: it does not depend on the
testcontainers reaper (ryuk), which is only a backstop for hard kills and cannot
be relied on alone. In some environments (for example Docker Desktop on WSL2)
ryuk is disabled or torn down before it can reap, so a container with no
in-process teardown leaks and stays running indefinitely.

That contract is **enforced, not documented**. `NewDSN` refuses to provision a
database for a test binary that is not running under `Main`, and fails with the
`TestMain` to add. The paragraph above was already true and already written when
three packages drifted off it anyway, leaving one stray container each per gate
run; a doc comment is not a mechanism. The check sits exactly on the defect,
since the call that would start an unreclaimed container is the one that fails,
inside the offending package.

For orphans left by a genuinely hard kill (a `SIGKILL` or a Docker restart before
either mechanism fires), sweep them with `make clean-testcontainers`. It
force-removes leftover Postgres test containers, scoped by the testcontainers
label and the `postgres:18` image so it never touches the compose dev stack.

## Three performance instruments, two of which gate

Performance has three instruments in this repo, and they are deliberately not the same
thing. Knowing which answers a given question is most of the value:

| | **Round-trip counting** | **Access-path assertions** | **Wall-clock benchmarks** |
|---|---|---|---|
| Catches | N+1s: cost that grows with row count | A read that cannot reach the index it was built for | Cost inside one statement: a recursive CTE that stops being bounded, a plan that degrades on real volume |
| Blind to | Anything a single statement does | Everything except one relation's access path | Anything smaller than the host's own noise |
| Determinism | Exact. Same number every run | Exact. Independent of the fixture's size | Noisy. Interpreted by comparison, never by threshold |
| Gates a merge | **Yes.** Ordinary tests in `make test` | **Yes.** Ordinary tests in `make test` | **No.** Inert without `-bench` |
| How you run it | `make test` | `make test` | `make bench`, deliberately, before and after |

They are complements, not alternatives. A dropped index issues exactly as many statements
as the index scan it replaced, so counting never moves; an N+1 that doubles from twenty to
forty statements is well inside the wall clock's noise on a laptop, so the benchmark never
notices; and neither of those can tell an index that is present from an index that is
reachable. Each is the only instrument that sees its own class.

### Asserting an access path, never a plan

`internal/storage/storagetest/accesspath` is the primitive, and the distinction it rests on
is the whole design ([ADR-0094](/architecture/decisions/#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing),
as amended). A plan a fixture PREFERS says nothing about production, because preference is a
function of table statistics and no fixture here is production-sized. Whether a query can
REACH an index at all is a different question: planned with `set enable_seqscan = off`,
which prices the sequential path out rather than forbidding it, the answer is stable across
an empty table, a fixture with no statistics, and the same fixture analyzed, while the join
shapes around the scan move freely between those states.

That is the failure this catches and nothing else can: an index sits in `pg_indexes` while
the read it exists for cannot use it, because a predicate got a function wrapped around it,
a type coerced, a leading column dropped from the filter, or (for a partial index) its
predicate stopped being provable from the query's own clauses. The statement count is
identical throughout, and so is the catalog.

Three rules, and an assertion that breaks one of them is worse than none:

1. **One relation's access path, never the plan's shape.** Everything above the scan is
   stats-dependent and will flap.
2. **Assert the index CONDITION, not only the index name.** A scan node can name an index
   and carry no condition, meaning the planner walks the whole index and filters afterwards.
   That is the shape a coerced predicate produces, and it reads as a pass to anything that
   greps for the name. `Plan.MustReach` checks the name, the condition, and that the node is
   not one the planner was forced into.
3. **Explain the statement the code really issued.** `querycount.Counter.Calls` hands back
   each captured statement WITH its arguments, so the guard replays the gateway's own SQL
   rather than a copy maintained beside it; a copy is exactly what stops failing when the
   original changes.

### Benchmarks are diagnostic, never a gate

`make bench` runs `go test -bench` over `internal/storage`, at a small estate and a larger
one, against real Postgres through the same harness the integration tests use. What it
deliberately does **not** do is as much of the design as what it does:

- **No CI job and no merge gate.** A wall-clock threshold on a shared runner either sits so
  loose it catches nothing or it flakes and gets muted, and a perf job everybody ignores is
  worse than none because it reads as coverage.
- **No stored baseline.** Comparison is between two runs *you* took, on one machine,
  minutes apart, with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
  (a separate install: `go install golang.org/x/perf/cmd/benchstat@latest`). A baseline
  committed to the tree would be a number from a different machine on a different day.
- **No timing assertion, anywhere.** Not in a benchmark, not in a test. Timing assertions
  flake on a dev laptop, and that flake is the reason the counting instrument exists.

Three rules make a benchmark here mean anything:

1. **Setup is outside the timed section.** Provisioning a database and seeding it costs far
   more than any query being measured, so a benchmark that includes its own fixture build
   measures the harness and moves whenever the harness does. Fixtures are built once per
   size and shared; `b.ResetTimer` runs before the loop.
2. **Two sizes, always.** One number cannot tell a constant apart from a linear cost, and
   the growth curve is the whole question: a list *should* grow with the estate, a tag
   cascade walking one component's ancestors should *not*, and only the pair says whether
   that is still true.
3. **Subtract the floor.** `BenchmarkRoundTripFloor` measures one pool acquire and one
   empty statement, and every other number contains one copy of it per statement issued. A
   call that issues dozens of statements is mostly transport, so a plan regression inside
   it moves the total by very little. That is a real limit on what this instrument can see,
   and a path measured to sit past it should be counted rather than timed.

## Counting round trips, not timing them

Read performance is asserted as a **count of SQL statements**, not as a duration.
The Gateway's dominant cost is round trips to Postgres, and the regression that
hurts at estate scale is the N+1: fifteen thousand components paying two or three
queries each. A count is deterministic. It needs no stored baseline, no threshold
policy, and no warm-up, and it fails with an exact number that names the defect,
where a wall-clock measurement on a laptop or a shared runner has variance that
swamps anything short of a catastrophe. What a count cannot see is a missing index
or a sequential scan inside one query; that is the other instrument above, not a
reason to skip this one.

`internal/storage/storagetest/querycount` is the primitive.
`storagetest.NewCountingDB` hands back a gateway plus a `querycount.Counter`, and
the counter is a `pgx.QueryTracer` attached to the pool's connection config, so it
sees every statement the gateway issues.

**The seam is the whole design, and it fails silently.** A count means nothing if
the code under test does not go through the counted seam: a counter wrapped around
some argument that the code never reaches for reports a small, flat, entirely
fictional number, and the assertion built on it reads as coverage while measuring
nothing. The gateway's `List` methods take a `scope.Set` and query the pool
directly, so only the pool tracer observes them; `Counter.Wrap` observes only a
function that takes its querier as a parameter, such as the address-attach hooks.
A count of zero where several statements were expected is the tell.

Every count assertion checks **both** properties, because neither alone has teeth:

- **Flatness**: the same cost at a small page and a larger one. A ceiling alone
  passes an implementation that is flat at a bad number.
- **A ceiling**: an absolute maximum. Equality alone passes an implementation that
  is still per-row with a smaller constant, and passes anything at all if the two
  pages are secretly the same size.

The fixture has to vary the dimension the count grows along, which is subtler than
page size: an address walk grows with the number of distinct rooms a page spans, so
a page confined to one room makes a per-room loop look exactly as flat as a batch.
And a new assertion earns its place by **mutation**: break the batching or the join,
watch the number move, revert. A count test whose only red was a build failure has
not been shown to measure anything.
