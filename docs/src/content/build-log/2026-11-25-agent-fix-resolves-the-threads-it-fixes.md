---
title: "agent-fix replies on and resolves the threads it fixes (#2217)"
date: 2026-11-25
refs: [2217, 2253]
---

`agent-status.sh replies` now answers every review thread the agent read, once per thread: `Fixed in <sha>`
and resolve for a fixed one, the reason and the thread left open for an unchanged one, and resolve for an
advisory finding the agent marks `no_change_needed`. The workflow fetches the PR's threads, posts the replies
and resolves with the agent App's token. The reviewer resolving its own threads that a later review no longer
raises landed with #2253.
