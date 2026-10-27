---
title: "Terminal demos pause after each command, so the inline clip reads (#1247)"
date: 2026-10-27
refs: [1247, 1132, 1180]
---

A terminal demo's GIF plays inline in the PR body, where a reader cannot pause it, and the tapes
went straight from one answer to typing the next. One pacing rule now holds every line a tape shows
before the prompt comes back: a command's output for 2.5s plus 0.5s per row it filled (up to 7s),
and a typed caption for 1.5s plus 0.2s per word (up to 4.5s). The numbers are `PACE` in
`web/e2e/tapelib.mjs`. The prelude applies them through `web/e2e/tape/pace.sh`, running the shell
under script(1) so the rows are counted from what the terminal was actually sent, and it lists each
hold in the recording's `pace.txt`.

A tape can no longer add, skip or override a hold. The house rules refuse a `Sleep` (all 60 hand-placed
ones are gone from the 14 tapes) and any typed text that reaches the rule's hooks: the prompt
variables, the prelude's own names, the shell's history, or an `exec`, a `trap` or a new interactive
shell, including one reached through a path, `env`, `command` or `builtin`. A caption runs nothing, so
it is exempt. `make test-demos` now proves that each caption and command a passing tape shows is
held for at least the rule's base time, and that the video runs at least as long as its holds.

The terminal demo budget rises from 120s to 180s
([ADR-1247](/architecture/decisions/#adr-1247-terminal-demos-are-paced-by-one-rule-and-a-terminal-demos-budget-is-180-seconds)).
`make test-web` adds up the least each tape can run and fails one that is already over budget before
a stack starts. Re-recorded under the rule, the 14 tapes run 56s to 163s, with GIFs from 0.3 to
1.1 MB. Stepping through the frames, each answer stays on screen, whole, before the next command types.
