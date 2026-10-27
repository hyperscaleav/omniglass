---
title: "A demo is one concept with two recorders, and every PR demos its feature"
date: 2026-10-25
status: "Accepted"
summary: "Every PR carries a demo, a recording of its feature working: a terminal demo (a VHS tape, `scripts/demo/<id>.tape`, played by VHS from its own image, pinned by digest, against the walkthrough stack) for CLI and API work, a walkthrough for a console flow, n/a only with a reason. Both recorders share one label (`run:demo`, which keeps honoring `run:walkthrough`), one record job, one publish job over the existing evidence ref, one prune and one PR comment; a terminal demo's GIF plays inline from the raw host and its MP4 scrubs through jsDelivr. A tape is held to house rules and house settings, and a command in it that fails fails the demo"
refs: [1132, 939, 1131]
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [demos](/contributing/demos/), [labels](/contributing/labels/), [slice workflow](/contributing/slice-workflow/), [docs with everything](/contributing/docs-with-everything/)
- **Decision:** A **demo** is a recording of a PR's feature working, made by one of two recorders,
  and a demo id names one demo of either kind (#1132):
  1. **Two recorders.** A terminal demo is a VHS tape under `scripts/demo/`, typed into a real
     terminal by the CLI the PR builds, against the walkthrough stack, and rendered as a GIF and an
     MP4; a walkthrough stays the Playwright script it was (#924). VHS runs from its own image
     (`ghcr.io/charmbracelet/vhs`, v0.12.1, pinned by digest), which bundles ttyd, ffmpeg, chromium
     and the fonts; the recorder adds `jq`, a prelude and this tree's `omniglass` binary
     (`web/e2e/tape/Dockerfile`).
  2. **One pipeline.** `run:demo` records the PR's demos of both kinds in the one record job (the
     PR's code, a read-only token), and the one publish job commits them to the PR's existing
     evidence ref, `refs/walkthroughs/pr-<n>`, under the existing 30-day prune
     ([ADR-0149](#adr-0149-walkthrough-evidence-lives-on-per-pr-refs-its-pr-video-plays-through-jsdelivr-and-the-docs-commit-the-mp4)).
     It upserts **one comment** listing every demo: a terminal demo's GIF from the raw host,
     playing inline, with its MP4 linked through jsDelivr to scrub; a walkthrough as before. The
     workflow keeps honoring `run:walkthrough`, and the upsert finds the walkthrough comment's
     earlier marker, so a PR labelled before this keeps one comment.
  3. **The house rules.** A tape sets nothing about its look (one size, font, theme and typing
     speed, applied by the recorder) and may use only the commands a demo types and waits on;
     every `Enter` is waited on; no uuid is typed (address by name or dotted address, or look an
     id up by name in the tape). The prelude lists every typed command that exits non-zero, and
     any listed command fails the demo, as do a `Wait` that runs out and a recording over budget
     (a GIF under 5 MB, under two minutes; waits are hidden and cost nothing).
  4. **The rule.** Every PR demos its feature: a terminal demo for CLI and API behavior, a
     walkthrough for a console flow, both when it has both, `n/a` only with a reason.
     `/ship-slice` refuses a ship-review with no Demo line, and the PR template carries a Demo
     section.
- **Context:** #1131's demo was a pasted transcript, the first, because no terminal recorder was
  installed, while console flows already played on the PR through the walkthrough pipeline (#939).
  The architect asked for every PR's demo to play as an animated terminal recording, and, in
  session, that the terminal recorder join the walkthrough pipeline rather than sit beside it as a
  second one: one label, one record and one publish job, one prune, one comment. A sibling
  `run:demo` beside `run:walkthrough` was weighed and rejected: two labels would mean two comments,
  or two triggers for one act of review, and the cost they would have separated is handled by the
  selection instead (a CLI-only PR records only its tapes, never every walkthrough; a console change
  still records every walkthrough). VHS's own image was preferred over driving the chromium the
  Playwright image already ships: the image is the combination VHS is built and tested with,
  publishes amd64 and arm64, needs nothing installed on a host, and renders the same fonts on a
  laptop and in CI, while the Playwright route would hand-pin VHS, ttyd, ffmpeg and a font into a
  derived image and point go-rod at a browser it does not look for. Its cost is one more image pull
  (about 3 GB) per CI run that records a tape. asciinema was not needed. The evidence ref keeps the
  name it had when walkthroughs were the only demos, so the prune and every earlier comment stay as
  they are.
- **Amended (#1180):** the selection is explicit. A PR records exactly the demos its body tags
  (`demo: <id>`, or `demo: none (<reason>)`) plus any it adds or changes; the widening on a recorder,
  `web/` or stack change, and recording every demo when a PR names none, are retired, and a nightly
  run on main re-records every demo instead. Each demo records on a runner of its own, and the
  publish job also writes each demo's clip into the PR body. See
  [ADR-1180](#adr-1180-a-pr-records-only-the-demos-it-tags-carries-its-clip-in-its-body-and-main-re-records-every-demo-nightly).
- **Amended (#1247):** a terminal demo's length budget is 180s, not two minutes, and one pacing
  rule holds each line a tape shows before its prompt returns (a tape writes no `Sleep`). See
  [ADR-1247](#adr-1247-terminal-demos-are-paced-by-one-rule-and-a-terminal-demos-budget-is-180-seconds).
