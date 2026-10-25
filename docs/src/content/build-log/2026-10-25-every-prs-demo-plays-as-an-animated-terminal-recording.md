---
title: "every PR's demo plays as an animated terminal recording (#1132)"
date: 2026-10-25
refs: [1132, 1131, 939]
---

A demo is now one concept with two recorders
([ADR-1132](/architecture/decisions/#adr-1132-a-demo-is-one-concept-with-two-recorders-and-every-pr-demos-its-feature)):
a **terminal demo**, a VHS tape under `scripts/demo/`, for CLI and API work, and the walkthrough it
already was for a console flow. Every PR demos its feature, and `/ship-slice` and the PR template
carry a Demo line it refuses to go without; the [demos](/contributing/demos/) page (the walkthroughs
page, renamed) teaches both.

The terminal recorder (`web/e2e/tapes.sh`) brings up the walkthrough stack, builds VHS's own
image, pinned by digest, with `jq`, a prelude and this tree's `omniglass` binary added
(`web/e2e/tape/Dockerfile`), and follows the stack's container logs into files a tape can read.
Each tape is held to the house rules and wrapped in the house settings
(`web/e2e/tapelib.mjs`: only the commands a demo types and waits on, every `Enter` waited on, no
uuid typed, one size, font, theme and typing speed), played on the stack network with the CLI
pointed at the stack, and judged: the prelude lists every command the tape typed that exited
non-zero, and any listed command, a `Wait` that ran out, a missing GIF or MP4, or a recording over
budget fails it, naming the tape and the cause. In a `Hide` block, `demo_until` and `demo_hush`
wait and look up unseen and erase their own lines, so a demo's waits cost the GIF nothing.
`make demo ID=<id>` records a demo of either kind (`web/e2e/demo.sh` dispatches by id), and
`make test-demos` proves every failure path against the real stack.

The pipeline is the walkthrough one, widened rather than doubled: `demo.yml` (renamed from
`walkthrough.yml`) runs on `run:demo`, and still on `run:walkthrough`, records the demos the PR
names (every tape when it changes the terminal recorder, every walkthrough when it changes the
console), and its publish job commits them to the same `refs/walkthroughs/pr-<n>` under the same
prune (`demo-prune.yml`). One comment lists every demo, a terminal demo's GIF inline with its MP4
linked through jsDelivr, beside the walkthroughs; the upsert finds the walkthrough comment's
earlier marker too, and now matches a marker only at the start of a bot comment.

#1131's scripted session, `http-logins.sh`, became the first tape, `http-logins.tape`: the panel and
the display by dotted address, the ids the log grep needs looked up by name unseen, the polls'
waits hidden, and only fields that hold still on screen.

Proven in `web/src/tape.test.ts` (the rules, the wrap, the verdict, the manifest, and every tape in
the tree), `web/src/walkthrough-pr.test.ts` (the selection across both recorders, a terminal demo's
evidence files, and the one comment), and `make test-demos` against the real stack (a passing tape,
a command that fails, an unseen wait that runs out, a command stuck past its `Wait`, and an
over-budget recording).
