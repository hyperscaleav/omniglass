---
title: "The test and capture stacks run side by side (#884, slice 1: #855 and #771)"
date: 2026-10-19
refs: [884, 855, 771]
---

`make test-e2e`, `make docs-shots` / `docs-shots-check` and `make test-netadmin` share one stack helper
(`web/e2e/lib/stack.sh`): every container and network takes a per-run id and a label, teardown stops
only its own on exit, interrupt or termination, and a run killed outright is swept by the next run with
the same prefix once its owning process is gone. Two worktrees running the same target at once no longer
destroy each other (both proven green at once), and a stack disturbed from outside mid-run fails saying
so rather than reporting the resulting broken capture as pixel drift. The old recipes also leaked
Postgres's anonymous volume on every run; containers now start with `--rm`. This is the stack the
walkthroughs (#923) build on.
