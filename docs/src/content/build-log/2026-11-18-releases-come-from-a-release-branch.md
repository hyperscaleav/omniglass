---
title: "Releases come from a release branch (#2139)"
date: 2026-11-18
refs: [2139, 1940, 1988, 1257]
---

The release workflow runs only on a `release/<major>.<minor>` branch (ADR-2139). A dispatch tags the
branch tip as the next candidate, `v0.1.0-rc.1`, and publishes it as a prerelease with its image and
binaries; a fix reaches the branch by cherry-pick from `main` and the next dispatch is `rc.2`; a
dispatch with `final` tags the newest candidate's commit `v0.1.0` once it has passed the soak gate.
`cmd/releaseversion` works the tag out from the branch name and the tags already cut, so
semantic-release and `.releaserc.json` leave the release path, and the notes are GitHub's generated
notes since the previous final release. Pushes to a release branch run the same CI as `main`. The
tag is pushed with a release GitHub App's token when one is configured, the one bypass the `v*`
creation ruleset will allow (#1940).
