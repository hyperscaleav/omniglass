---
title: "The CI gate is make test, and every action is pinned (#214, #66)"
date: 2026-10-26
refs: [214, 66, 1174, 825]
---

Pre-tag hygiene for 0.1.0 (#825), the CI half. `test.yml` now runs `make test`, the gate
contributors run, instead of its own `go test ./...`: the SPA typecheck and vitest suite join CI
for the first time, beside every Go tier and the NET_ADMIN tier, so a web-only regression fails the
PR that introduces it (#214, which #846's calendar rot showed was not hypothetical). The job raises
`TEST_PARALLEL` to the runner's core count, which is what the bare `go test` ran with. The
`run:test` and `run:build` labels it would have replaced are retired from the label docs, and
CONTRIBUTING no longer says the full suite is label-gated.

Every workflow's actions, first-party `actions/*` included, are pinned to a full commit SHA with
the version in a trailing comment, and `.github/dependabot.yml` keeps the pins and the Go and npm
dependencies current through weekly grouped PRs whose titles pass the PR-title gate (#66). The
[release and CI](/contributing/release-ci/) page teaches both.

Two stray files leave the tree: the 5.5 MB `cligen` binary at the root, already covered by the
anchored ignore #1162 added (#1174), and an empty root `package-lock.json`, now ignored too. The
roadmap's 0.1.0 row stops saying #603's stateful arc closes the epic: #603 is closed, and its
follow-ups live on #1187.
