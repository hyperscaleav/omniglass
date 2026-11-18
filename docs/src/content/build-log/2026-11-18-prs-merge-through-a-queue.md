---
title: "PRs merge through a merge queue (#2181)"
date: 2026-11-18
refs: [2181, 2180]
---

Every workflow that reports a required check on `main` (`test`, `e2e`, `gen-drift`, `smoke-lab` and
`lint`) now also runs on `merge_group`, so GitHub's merge queue can test each PR on top of the
current `main` and squash-merge it when the checks pass there. The `pr-title` and `demo-tag` lints
pass in the queue without reading a PR, having checked it before it was queued, and `image.yml` runs
only `smoke-lab` there and publishes nothing. A guard test in `internal/build` fails when a required
check's workflow lacks the trigger. Turning the queue on, and retiring the strict up-to-date rule
and the always-on admin bypass it replaces, are ruleset settings that follow the merge (epic #2180).
