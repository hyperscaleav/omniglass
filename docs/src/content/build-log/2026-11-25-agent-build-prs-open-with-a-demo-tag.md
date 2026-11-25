---
title: "agent-build PRs open with a demo tag (#2345)"
date: 2026-11-25
refs: [2345]
---

The agent-build prompt has the agent build `body.md` from the PR template, fill Demo, run the demo tag
lint and open with `--body-file`. The expired-token path lints the body it opens from, using main's copy
of the lint staged before the agent runs; a failing body opens the rescue draft instead. The rescue body's
tag is pinned by a test.
