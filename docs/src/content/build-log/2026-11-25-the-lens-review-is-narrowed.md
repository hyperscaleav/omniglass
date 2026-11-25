---
title: "The lens review is narrowed: invariants split by area, a scoped neighbor grep, a one-commit checkout (#2216)"
date: 2026-11-25
refs: [2216, 2211]
---

The lens review (the `review:deep` label, or `AGENT_REVIEW_MODE` set to lens) deals `invariants` for code outside
`web/src` and `invariants-web` for `web/src`, each only when the diff touches its area. The `neighbor` lens greps the
docs pages and the decision files (not the build log or the generated pages). The lens jobs check out one commit and read diffs `plan` computes once (`pr.diff`, `review.diff`).
The default one-shot reviewer is unchanged.
