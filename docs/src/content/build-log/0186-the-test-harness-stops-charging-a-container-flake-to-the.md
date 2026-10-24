---
title: "The test harness stops charging a container flake to the gate"
date: 2026-08-10
refs: []
form: bullet
---

- **The test harness stops charging a container flake to the gate**
  ([#661](https://github.com/hyperscaleav/omniglass/issues/661),
  [#663](https://github.com/hyperscaleav/omniglass/issues/663)). Two defects in `storagetest`
  container hygiene, shipped together because each fed the other. The container start still sat
  behind a `sync.Once` that stored its error, the exact shape
  [#649](https://github.com/hyperscaleav/omniglass/issues/649) had just removed from the template
  build one layer up, so a single transient start failed every remaining test in the binary with
  the same replayed message at 0.00s each. It now caches success and never failure, behind a mutex,
  and the next test retries. Retrying exposed the second half: `testcontainers` hands the container
  back **alongside** the error when the create succeeded and the wait strategy did not, so the
  caller that only checked the error was dropping a live Postgres, and a retry loop would have
  leaked one per attempt. `startContainer` now reclaims whatever it was handed, and returns a nil
  container on every error path as a contract its callers depend on.

  The other half was three packages, `internal/bus`, `internal/cli` and `internal/node`, that used
  the harness without routing `TestMain` through `storagetest.Main`, leaving cleanup to a reaper
  this box cannot trust. A gate run with `TESTCONTAINERS_RYUK_DISABLED=true` left exactly three
  strays, one per package; the same run after the fix leaves zero, measured both times. The three
  `TestMain` functions are the small part. The load-bearing part is that the requirement stopped
  being a doc comment: `NewDSN` refuses to provision for a binary that is not running under `Main`
  and fails with the fix in the message. The guard sits exactly on the defect, since the call that
  would start an unreclaimed container is the one that fails, in the offending package, and it runs
  ahead of the `-short` skip so `make test-short` catches an omission too. A static walk of the
  importers was the alternative and was rejected: it would have had to parse the module from inside
  a test to answer a question the runtime already knows, and a guard against flaky gate readings
  that can itself flake is not worth having.

  Every claim here is proven by mutation. Caching the start error again fails the recovery
  assertion; dropping the terminate on the failed-start path leaves the injected container
  inspectable; removing `internal/bus`'s `TestMain` turns that package red on its first provision.
  The start failure is injected through a seam rather than provoked, because Docker cannot be asked
  for a half-started container on demand, while the real start and terminate stay covered against
  real Docker by the tests that were already there.
