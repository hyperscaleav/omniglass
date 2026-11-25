---
title: "The docs-shots recapture runs when the seed or migrations change (#860)"
date: 2026-11-25
refs: [860, 2561]
---

The recapture job's push trigger now includes the dev seed, the boot seed, the migrations, the Makefile, the
Dockerfile and the workflow, the inputs a capture renders from. A seed-only change that moves a shot is
recaptured on main after it merges. The pull request filter is unchanged: since #2561 a PR compares no pixels.
