---
title: "Agent PRs run the required checks before they push"
date: 2026-11-19
refs: [2231, 2232]
---

A new `make preflight` runs what a PR's required checks run (make gen with a drift check, make
test, the docs build and the migration check) and both agents run it before every push. agent-fix
now merges main into the PR's branch before the agent starts and hands it any conflicts first, so
a stale branch no longer stays stale. An agent's PR no longer carries main's copies of the agents'
own config: preflight fails a run that commits them, and agent-fix puts main's copies back if any
slip through.
