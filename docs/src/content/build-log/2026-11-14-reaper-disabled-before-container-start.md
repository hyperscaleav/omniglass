---
title: "Disable testcontainers reaper to avoid transient startup failures (#1411)"
date: 2026-11-14
refs: [1411, 1410, 661]
---

The testcontainers reaper (ryuk) experiences transient startup failures on shared CI runners where network overhead is high.
On a loaded bridge host, the reaper can exceed its startup deadline and fail an entire test binary even though the actual
test logic is sound. This is a blast-radius problem: one test binary's transient infrastructure hiccup now cascades to all
remaining tests in that package.

The reaper is disabled (`TESTCONTAINERS_RYUK_DISABLED=true`) because two load-bearing cleanup paths already exist and are
sufficient: the storage harness itself terminates the container in-process via `Main` during test exit, and `make clean-testcontainers`
is the secondary backstop. The harness `ensureContainer` function now sets the env var programmatically before starting the container,
so the reaper is disabled regardless of how the test is invoked (Makefile, direct `go test`, or CI). This ensures that a transient
infrastructure flake costs a single test's retry rather than the entire test suite.
