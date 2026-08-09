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
`internal/storage/storagetest`. It starts a single container per test binary
(lazily, via `sync.Once`) and hands each test a fresh, migrated, isolated
database, so tests never share mutable state or collide on a host port.

The migration chain is applied **once per test binary**, into a template
database, and each test's database is a `CREATE DATABASE ... TEMPLATE` copy of
it. A copy is a file-level clone rather than a replay of every migration, and it
carries `schema_migrations` with it, so a provisioned database is
indistinguishable from a migrated one, including to dbmate. Isolation is
unchanged: every test still gets its own database.

Building the template is per-binary work, so the harness caches its **success
and never its failure**. A `sync.Once` here would remember one transient hiccup
(a flapping container, a refused admin connection) and fail every remaining test
in the binary from it; retrying on the next call keeps a blip costing one test,
which is what the per-test migration replay used to give for free.

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
in-process teardown leaks and stays running indefinitely. A new harness-using
package that omits its `TestMain` reintroduces that leak.

For orphans left by a genuinely hard kill (a `SIGKILL` or a Docker restart before
either mechanism fires), sweep them with `make clean-testcontainers`. It
force-removes leftover Postgres test containers, scoped by the testcontainers
label and the `postgres:18` image so it never touches the compose dev stack.

## Counting round trips, not timing them

Read performance is asserted as a **count of SQL statements**, not as a duration.
The Gateway's dominant cost is round trips to Postgres, and the regression that
hurts at estate scale is the N+1: fifteen thousand components paying two or three
queries each. A count is deterministic. It needs no stored baseline, no threshold
policy, and no warm-up, and it fails with an exact number that names the defect,
where a wall-clock measurement on a laptop or a shared runner has variance that
swamps anything short of a catastrophe. What a count cannot see is a missing index
or a sequential scan inside one query; that is a different instrument, not a reason
to skip this one.

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
