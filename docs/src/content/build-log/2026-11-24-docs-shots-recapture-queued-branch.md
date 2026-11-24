---
title: "The docs-shots recapture survives a queued PR and skips noise (#2602)"
date: 2026-11-24
refs: [2602, 2561]
---

The `recapture` job no longer fails main when its open PR is in the merge queue: it pushes to a fresh
`claude/docs-shots-recapture-<short sha>` branch and opens a new PR (or exits green when the queued PR
already carries the shots). It commits only the shots whose masked baseline changed, plus their clean
twins, and changes nothing when no baseline moved. The decisions live in `scripts/docs-shots-plan.sh`,
unit-tested in `scripts/docs_shots_plan_test.go`.
