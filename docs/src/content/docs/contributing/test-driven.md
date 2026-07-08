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
