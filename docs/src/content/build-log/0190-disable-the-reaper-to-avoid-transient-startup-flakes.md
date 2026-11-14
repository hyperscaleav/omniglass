---
title: "The reaper is disabled to avoid transient startup flakes"
date: 2026-11-14
refs: []
form: bullet
---

- **The reaper is disabled to avoid transient startup flakes**
  ([#1411](https://github.com/hyperscaleav/omniglass/issues/1411)). Under loaded `make test`
  runs on shared hosts (notably the CI bridge), `testcontainers` ryuk reaper startup would occasionally
  timeout and fail the first test that hit the container start, wasting the entire package. The reaper
  is a hard-kill backstop for cleanup when a test binary crashes, but in this environment the startup
  cost is prohibitive and the startup failure is transient.

  `TESTCONTAINERS_RYUK_DISABLED=true` is now set by the Makefile test targets (`test-go-storage`,
  `test-go-devseed`, `test-go-rest`), disabling the reaper altogether. The harness has two load-bearing
  layers: `Main` terminates the container in-process on normal exit (enforced by a guard that prevents
  `NewDSN` from provisioning for a binary that did not run under `Main`), and `make clean-testcontainers`
  is the secondary leak backstop for escaped containers. A test-time reaper failure that was not caught
  by the integration test `TestStartContainerTerminate` (against real Docker) would not be caught here
  either; the switch trades that property for freedom from transient startup failures on a loaded box.

  Commit [#661](https://github.com/hyperscaleav/omniglass/issues/661) tested a gate run with the same flag.
