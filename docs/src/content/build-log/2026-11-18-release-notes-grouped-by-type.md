---
title: "Release notes are grouped by type, and a release marks what it shipped (#2142)"
date: 2026-11-18
refs: [2142, 2139]
---

A release's notes now come from `cmd/releasenotes`, not GitHub's generated notes: every PR title
since the previous final release, grouped into Breaking changes, Features, Fixes, Performance and
Other changes by its conventional-commit type, with a compare link at the end. A `dry_run`
dispatch shows them in the run's summary. A final release then comments "Released in" on each PR
it ships and each issue that PR closed, once per release; candidates comment nothing, and a
release of more than 100 PRs, like the first, skips the comments. These were the two parts of
semantic-release worth keeping once the version moved to the release branch (#2139). Nothing is
committed back.
