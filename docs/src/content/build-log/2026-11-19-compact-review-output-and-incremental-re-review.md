---
title: "Compact review output and incremental re-review (#2224)"
date: 2026-11-19
refs: [2224]
---

`agent-review.sh` previously posted a dense body that buried the verdict and repeated finding
details twice (once inline, once in the body). Large PRs could also re-review code the reviewer
had already signed off on, since every run diffed from the merge base regardless of what had
changed since.

The review body now opens with a one-line verdict (`✅ Ship · nothing found`,
`❌ Changes requested · N blocking · M advisory`, or `⚠️ N advisory`), followed by a
per-lens icon strip, then all details in a collapsed `<details>` block. Inline comments use a
bold title and a collapsed block instead of a wall of text. `open_threads` deduplication
drops any finding whose `path + line + rule` already has a live reviewer thread, so a re-run
only surfaces new problems.

Two new subcommands support the efficiency work: `turns N` prints the turn limit for a diff of
N lines (40 / 60 / 80 / 120 as size grows, instead of a flat 80); `incremental-base` reads the
reviewer bot's review history and returns the commit the bot last reviewed, so the workflow can
narrow the diff to only new changes. The `lenses` subcommand gains an optional diff-file
argument: a diff with no Go or web code skips the catalog shards and invariants (which check
Go/web anti-patterns) and runs the neighbor check only.

`agent-trust.sh verdict` gains backward-compatible support for the new body format: its regex
for reading the blocking count from a COMMENT review now matches both `agent-review: N blocking`
(old) and `❌ Changes requested · N blocking` (new).

Workflow wiring for the incremental path (`turns`, `incremental-base`, `open_threads`,
diff-file filtering) requires `.github/workflows/` changes that this run could not push; they
are tracked in #2224.
