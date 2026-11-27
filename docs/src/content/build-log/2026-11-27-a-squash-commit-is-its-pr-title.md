---
title: "A squash commit is exactly its PR title (#2792)"
date: 2026-11-27
refs: [2792, 2652]
---

The repository squash-merges with the PR title as the commit's title and a blank body. The old
setting took a single-commit PR's subject from its commit, and listed every branch commit in the
body, where release-please counted each `* feat:` or `* fix:` line as a release-worthy change of
its own; now it reads one entry per PR, the title `pr-title.yml` lints.
