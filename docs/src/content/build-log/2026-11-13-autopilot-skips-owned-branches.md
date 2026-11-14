---
title: "The autopilot skips branches another session owns (#1112)"
date: 2026-11-13
refs: [1112, 1037, 1105, 1106]
---

The scheduled autopilot no longer adopts a branch a live session is still building. A session
building #1037 sat idle behind a rate limit with its branch unpushed; the autopilot copied it,
shipped it as #1105, and the original session then opened #1106 for the same issue.
`scripts/branch-owned.sh <branch>` now answers `owned <reason>` or `free`: a branch is owned when
it is checked out in a locked worktree, has a claim file beside its worktree, or saw a commit or a
file edit inside the last 24 hours (`AUTOPILOT_OWNED_WINDOW_HOURS`). The autopilot skill runs it
before resuming or adopting any existing branch, skips an owned one and fires the next rung,
comments on the issue before adopting a free branch it did not create, and lists every skipped
branch in the day report.
