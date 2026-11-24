---
title: "CI builds once and runs only what a push needs (#2597)"
date: 2026-11-24
refs: [2597]
---

`make SKIP_BUILD=1` (and `make test-e2e`, `make lab`, which depend on `all`) skips both docker builds
and only checks that `omniglass:dev` is loaded and `bin/omniglass` exists. On top of it, `test.yml`
gets a `changes` job (`scripts/ci-changes.sh`, tested) and a `build` job that makes the image and host
binary once with the gha BuildKit cache; `e2e` and `smoke-lab` load that build. A docs-only PR skips the
Go and web jobs, a web-only PR skips the Go ones, a draft runs the unit jobs only, and `main`, the merge
queue and a PR marked ready run everything. Required checks still report: `e2e`, `smoke-lab` and `gen-drift`
always run and pass when nothing needs them. `image.yml` publishes only (arm64 smoke on push and queue,
multi-arch push on `main` and release, an amd64 image for `run:preview`). `agent-review` waits for the test
checks on the same head and holds if they never finish; `agent-fix` merges `main` only for a conflict or a
red required check. Tests: `TestMakeAllSkipBuild`, `TestCIChanges*`, `TestAgentReviewGate`,
`TestAgentSyncNeeded*`. Divergence from the ticket: the multi-arch push does not run on merge queue entries
(a queue entry's commit may never land); the arm64 smoke does.
