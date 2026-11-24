---
title: "make all can reuse a prebuilt image (#2597)"
date: 2026-11-24
refs: [2597]
---

`make SKIP_BUILD=1` (and `make test-e2e`, `make lab`, which depend on `all`) skips both docker builds
and only checks that `omniglass:dev` is loaded and `bin/omniglass` exists, so a CI job that loaded one
shared build can run the lab jobs without rebuilding. The default is unchanged. This is the Makefile half
of the ticket; the workflow changes (build once, review gating, draft and path filtering, arm64 off PR
pushes, agent-fix merge policy) are `.github/workflows` edits the unattended run cannot make and remain
open on #2597. Test: `TestMakeAllSkipBuild`.
