---
title: "The reviewer answers thread replies and supersedes its old reviews (#2253)"
date: 2026-11-24
refs: [2253, 2225, 2619]
---

`agent-review.sh` gains `reply-gate`, `reply-prompt`, `reply-result`, `thread-of`, `supersede`, `rounds` and
`round-marker`; each review opens with its head on the first line and ends with a round marker that agent-fix
counts, so dismissing old reviews does not reset the five-round park. A thread verified fixed resolves whether
advisory or blocking. Divergence from the ticket: the head SHA rides the verdict's first line (not a line of
its own) so the body still opens with the blocking count that `agent-trust.sh verdict` and the tests read.
The workflow half (the `pull_request_review_comment` trigger and `reply` job, the supersede step, agent-fix's
round count) ships as a patch in the PR body, since the build token cannot change `.github/workflows`.
Tests: `scripts/agent_review_test.go` (red before the subcommands).
