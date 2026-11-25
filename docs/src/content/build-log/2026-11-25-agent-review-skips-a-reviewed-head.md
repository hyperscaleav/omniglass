---
title: "agent-review skips a head it already reviewed (#2277)"
date: 2026-11-25
refs: [2277]
---

`plan` asks `agent-review.sh seen` before any wait or model call. A head with a completed review by an App bot
(not pending, not dismissed, with the round marker) is not reviewed again: a PR run re-reads the ruling so the
required check keeps reporting, a manual run stops. synchronize, `review:deep` and a dispatch with `force: true`
always review. The default branch's scripts are now checked out before the decision.
