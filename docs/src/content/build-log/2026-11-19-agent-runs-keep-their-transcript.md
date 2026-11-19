---
title: "Agent runs keep what the agent did"
date: 2026-11-19
refs: [2230]
---

Each agent-build and agent-fix run keeps an artifact for 14 days with the agent's transcript and
a list of every shell command it ran with its exit code, then whether make preflight and make
test ran and how each last ended. The repo's artifacts are public, so the kept transcript drops
every tool's output and masks the run's own tokens by value and other secrets by shape. The
daily lessons sweep (#2235) reads it to tell an agent that skipped the gate from a flaky check.
