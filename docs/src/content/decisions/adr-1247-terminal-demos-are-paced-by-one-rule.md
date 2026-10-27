---
title: "Terminal demos are paced by one rule, and a terminal demo's budget is 180 seconds"
date: 2026-10-27
status: "Accepted"
summary: "After each line a terminal demo shows, the prelude holds the screen before the prompt returns: 2.5s plus 0.5s a row of output up to 7s, and a caption 1.5s plus 0.2s a word up to 4.5s (`PACE` in `web/e2e/tapelib.mjs`). A tape writes no `Sleep` and types nothing that reaches the rule's hooks. The terminal demo's length budget rises from 120s to 180s, and `make test-web` fails a tape whose least possible length is already over it. Amends ADR-1132"
refs: [1247, 1132, 1180]
---

- **Date:** 2026-10-27 | **Status:** Accepted | **Pages:** [demos](/contributing/demos/)
- **Decision:** (#1247, amending [ADR-1132](#adr-1132-a-demo-is-one-concept-with-two-recorders-and-every-pr-demos-its-feature).)
  1. **One pacing rule.** After every line a tape shows, the shell holds the screen before its
     prompt returns, so the tape's `Wait` waits through the hold: a command's output for 2.5s
     plus 0.5s a row it filled, up to 7s; a typed `# caption` for 1.5s plus 0.2s a word, up to
     4.5s; an unseen `demo_until` or `demo_hush` and a `clear`, not at all. The numbers live
     once, as `PACE` in `web/e2e/tapelib.mjs`, handed to the prelude as tables; the prelude
     (`web/e2e/tape/pace.sh`) counts the rows from what the terminal was sent, by running the
     shell under script(1). Each hold is listed in the recording's `pace.txt`.
  2. **A tape cannot bypass it.** The house rules refuse a `Sleep`, and typed text that reaches
     the rule's hooks: the prompt variables, the `DEMO_` tables, the prelude's `demo__` names,
     the shell's history, and an `exec`, a `trap` or a new interactive shell (behind a path,
     `env`, `command` or `builtin` too). A caption runs nothing, so it is not held to this.
  3. **The budget is 180s.** `MAX_SECONDS` for a terminal demo rises from 120 to 180; the GIF
     limit (5 MB) is unchanged. `make test-web` computes each tape's least length (its typing
     on screen at the house speed, plus the rule's least hold after each line) and fails one
     already over the budget, before a stack starts.
- **Context:** A demo's GIF plays inline in the PR body, where a reader cannot pause or scrub
  it, and the tapes moved straight from one answer to typing the next, so most of what a demo
  showed went by unread. Hand-placed `Sleep`s were sparse and uneven (60 across 14 tapes, 1s to
  4s, none after most commands). The architect asked for one rule over every tape rather than
  more `Sleep`s, applied where no tape can forget it. Counting the output's rows needs what the
  terminal was sent; a `tee` of the shell's output would have made every command's stdout a
  pipe (losing jq's colors and changing what an operator would see), and reading the cursor
  row undercounts any output that scrolls, so the shell runs under script(1) and the prelude
  reads its log between two invisible marks. The holds add 50 to 90 seconds to a tape of
  fifteen to twenty commands: re-recorded under the rule, the longest tapes ran past 120s, and
  splitting each into two demos would have split one feature across two clips. The 180s
  ceiling keeps the longest tape watchable while its GIF stays near 2 MB, since a held frame
  costs almost nothing to encode. A slower typing speed was weighed and left at 20ms: the hold
  after each line already gives the typed command its read time.
