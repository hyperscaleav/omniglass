---
title: "Compact review output and incremental re-review (#2224)"
date: 2026-11-19
refs: [2224]
---

agent-review's output was too long to scan, and every push re-reviewed the whole PR. Both are
fixed.

The review body now opens with one verdict line (`✅ Ship · nothing found`,
`❌ Changes requested · N blocking · M advisory`, or `⚠️ N advisory`), then a strip with each
lens's mark, then one line per finding that has no diff line. The lens notes, failures and fixes
sit in a collapsed `<details>` block. Inline comments open with the finding's title in bold, from
a new required `title` in the lens schema, and keep their reasoning collapsed.

A re-review starts from what changed. `agent-review.sh incremental-base` finds the head the
reviewer last reviewed, and `delta` diffs the PR's own commits since, with changes merged in from
`main` laid onto that head first so they drop out. Fred's review of the first cut: that diff should
be where a lens starts, not a fence. So the lenses read any code, pull up the whole PR, and may run
`go build`, `go vet` and `go test` (setup-go in the lens job), and raise a finding wherever the new
changes cause or expose a problem. A force push, an unreadable earlier review, or
one that left a blocking finding in its body falls back to a full review. The diff picks the
lenses (`lenses` runs `neighbor` alone when it touches no code) and the turn limit (`turns`, 40
to 120 by size).

Open findings carry forward, and this is where the first cut had a hole: it dropped a new finding
that matched an open thread, so a re-run that found an unfixed blocker again posted a clean review
and unblocked the PR. Now every unresolved reviewer thread counts in the verdict, read back by
`agent-review.sh threads` from a marker each inline comment carries, and a matching new finding is
only not posted twice. Each re-review with open threads also runs a `recheck` lens that judges every
one, outdated ones included, as fixed or open. A thread stops counting only when that lens says
fixed and no other lens raised it again, or a person resolves it; a thread it gave no word on still
counts. `agent-review.sh followups` turns its word into a reply on each thread, and the workflow
resolves the fixed ones as the reviewer.

Code here means Go, web, shell, SQL and the workflows, wider than the ticket's "Go or web": the
invariants lens found this PR's one real problem in a bash script.

agent-fix answers each point on one line (`✅ Fixed in <sha>` or `↩️ Not changed`) with the
reasons collapsed, from a points file the agent writes, and replies `✅ Fixed in <sha>` on each
thread it fixed. `agent-status.sh` counts a point as fixed only when its commit is one the run
pushed. `agent-trust.sh verdict` reads the blocking count in both the old and the new body.
