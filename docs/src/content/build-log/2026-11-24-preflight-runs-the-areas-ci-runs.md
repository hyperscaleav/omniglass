---
title: "make preflight runs the areas CI runs"
date: 2026-11-24
refs: [2629, 2597, 2231]
---

`make preflight` now sends the changed files through `scripts/ci-changes.sh` and runs only the
steps for the areas that come back (gen, web, go, docs), so a docs-only agent push no longer
waits on the Go suite. The agent-config guard still runs every time, and an unclaimed path still
runs everything. `scripts/preflight_test.go` holds each step to its area.

Not done here: stopping a preflight timeout after a landed push from leaving agent-fix red so that
agent-review holds. That change is in `.github/workflows`, which an agent run cannot edit; it is
left on #2629.
