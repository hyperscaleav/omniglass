---
title: Walkthroughs
description: "A walkthrough is a recorded, captioned operator flow, scripted as data and played against the real console: when a PR needs one, how to write, record and review it, and how a docs page embeds it."
---

A **walkthrough** is a short recording of an operator doing one thing in the console, from the
first click to the result, with a caption on every step. It is scripted as data
(`web/e2e/walkthroughs/<id>.yaml`), played by a recorder against the real console on the
[walkthrough stack](/guides/install/#the-whole-loop-in-docker) (a server, a collecting node and a
simulator, all in Docker), and written out as an mp4, an animated preview, a poster frame and one
still per step.

A screenshot shows a surface; a walkthrough shows a **flow**: what the operator does, in what
order, and what the console answers each time. It is also a test. Every action ends on a wait for
its visible result, so a step whose result never appears fails the recording, naming the step.

## When a PR needs one

A PR that **adds or changes an operator flow** carries a walkthrough: a form, a multi-step path,
a drawer action, a route, or the steps between them, anything an operator now does in a different
order or with different input. A visual-only change (a restyle, a new column, a moved badge) keeps
[screenshots](/contributing/docs-with-everything/#screenshots-are-generated-not-pasted) alone.
The `/ship-slice` gate decides it from the diff, not from intent, and a PR that needs a
walkthrough is not `ready` without one, exactly as a UI PR is not `ready` without screenshots
([the slice workflow](/contributing/slice-workflow/)).

## Write the script

A walkthrough is `web/e2e/walkthroughs/<id>.yaml`. The id is lowercase letters, digits and hyphens;
it names the script, its recording directory and its video. The script has a `title`, an optional
`description`, and `steps`: each step is one kind of action, or a wait for a result, with the
`caption` shown while it plays. The step kinds, their fields and the rules (no unknown key, no
script ending on an action, at most one `poster` step) are defined once, with an annotated
example, in the header of
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs),
the validator the recorder and the unit tests both run. This page does not restate them.

Write it the way an operator would work:

- **One flow, the shortest honest path.** No detour, no repeated step, no step the operator would
  not take; follow the order the console invites.
- **End every action on its result.** After a click, a typed value or a key press, wait for what
  the console shows in answer (the drawer open, the row added, the status changed). That wait is
  the assertion that makes the walkthrough a test.
- **Captions teach.** Say why the operator takes the step, in the product's vocabulary
  (component, place, system), short enough to read in the caption bar.
- **Mark the payoff.** `poster: true` on the step the walkthrough builds to makes its still the
  poster, the still that stands for the whole flow below its animated preview; without it the
  last step is the poster.

`make test-web` validates every script in the tree, so a malformed one fails before a browser
starts.

## The budget

A recording has a ceiling on its length, on the size of its mp4 and on the size of its animated
preview. The limits are defined once, as `MAX_SECONDS`, `MAX_BYTES` and `MAX_PREVIEW_BYTES` in
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs),
and are not restated here; a recording at or over any of them, or with no preview, fails, naming
which. Only a recording whose mp4 and preview both pass carries the budget mark (and the preview's
name) in its `walkthrough.json`, which is what lets the docs copy refuse one that did not. A
walkthrough that will not fit is two flows: split it.

## Record it locally

With nothing but Docker:

```bash
make walkthroughs ID=sample-simulation   # one walkthrough (ID="a b" for several, no ID for all)
make test-walkthroughs                   # the recorder's own test: the failure and budget paths
```

`make walkthroughs` brings up the walkthrough stack, plays each script in a Playwright container
with a visible cursor, a highlight on each target and a caption bar, and writes
`web/e2e/walkthroughs/out/<id>/` (gitignored): `<id>.mp4`; `preview.webp`, an animated preview
encoded from the mp4 at a lower frame rate and width that loops forever, small enough to load
inline on a PR or a docs page; `poster.png`; `step-NN.png` per step; and `walkthrough.json`, the
manifest of the title, the captions and the still each step produced (with the budget mark and
the preview's name added once the mp4 and the preview pass the budget). The preview's frame rate,
width and quality are set at the top of
[`web/e2e/walkthroughs.sh`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughs.sh).
The ids that failed are listed in `out/failed.txt`. Recording details, including the amd64-only
ffmpeg image, are in the [install guide](/guides/install/#recording-a-walkthrough).

## Show it on the PR

Push, then apply the **`run:walkthrough`** label. CI records the walkthroughs on the same stack,
commits each one's animated preview, poster and stills to the PR branch, and keeps one PR comment
that leads each walkthrough with its preview, playing on the PR page, then its poster and its
stills folded below, with the full-quality videos as a workflow artifact (GitHub plays no inline
video a workflow can attach, so the preview is what plays); a failed walkthrough fails the job and
is named in the comment. What the label runs, and where the stills land, is described with the rest of the
automation labels in [labels](/contributing/labels/). The PR body's Visual confirmation links that
comment on its **Walkthrough** line.

## Review the stills

A reviewer who cannot watch the video, an agent above all, grades the per-step stills instead:
open each `step-NN.png` in order beside its caption and check that each step lands on the result
its caption promises, that the cursor and highlight sit on what the caption names, that the
caption bar hides nothing the step is about, and that the poster is the moment the flow builds to.
The full rubric is the walkthrough section of the
[`/assess-ui` skill](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/assess-ui).
A failing line is a fix to the surface or the script, then a re-record and a regrade.

## Embed it in the docs

A docs page embeds a walkthrough the way it embeds a screenshot: it declares the id in
`walkthroughs` frontmatter and places it with a directive.

```markdown
---
title: Simulate a fleet
walkthroughs:
  - id: sample-simulation
---

::walkthrough{#sample-simulation}
```

The declaration is only the id. The title, captions and stills come from the recording's manifest,
so the page never restates a caption that could drift from the video. The embed renders the
animated preview (the poster step's still, for a recording whose manifest names no preview), the
title and description, and every step's caption over its still, folded under a disclosure. [Simulate a fleet](/guides/operator/simulation/#see-it) carries the first one.

The assets reach the site by a copy, committed like the screenshots:

```bash
make walkthroughs ID=sample-simulation   # record it
make docs-walkthroughs                   # copy what the docs declare into docs/public/walkthroughs/<id>/
```

`make docs-walkthroughs` copies, for every walkthrough a page declares, the manifest, the stills and
the animated preview from `web/e2e/walkthroughs/out/<id>/` into `docs/public/walkthroughs/<id>/` (not the recorder's
`poster.png`, a copy of the poster step's still), removes any file left from an older recording,
and fails on a declared walkthrough that is not recorded, whose recording failed, or whose manifest
lacks the budget mark. Commit the copied files with the change.

**Force the next docs build after every copy.** Astro caches each rendered page keyed on the page's
own content, and it has no way to learn that the embed also depends on the copied manifest and on
whether the video exists. An unforced build after a re-copy serves the old render, so build with
`corepack pnpm astro build --force` from `docs/` (the copy prints this too); the post-build check
below fails the build if a stale render slips through.

**The video is not committed**: an mp4 per walkthrough would grow the repository with every
re-recording. Its place is `docs/public/walkthroughs/<id>/<id>.mp4` (gitignored), and the embed
renders the video, with a link to it, only when that file is present at build time; without it
the page shows the animated preview and the stills, and links nothing. Until the docs deploy carries
the videos, the published pages show the preview and stills. To see the video in a local build, copy it
too with `DOCS_WALKTHROUGH_VIDEO=1 make docs-walkthroughs`.

Three checks keep the embed true to its recording:

- **Before the build**, `docs/scripts/check-embeds.mjs` fails a `::walkthrough` with no id, an id
  its page does not declare, or a declared walkthrough with no copied manifest.
- **In `make test`**, the docs lint suite (`internal/docslint/walkthroughs.go`) fails a declared
  id with no script under `web/e2e/walkthroughs/`, a declaration no directive on its page uses, a
  copied manifest with no budget mark, a missing still, a poster that is none of the stills, a
  preview the manifest names that is not `preview.webp` or was not copied, a copy
  that has drifted from its script (title, description, step count or any caption), and a copied
  directory no page declares.
- **After the build**, `docs/scripts/check-walkthrough-embeds.mjs` re-renders every built embed
  from the manifest the build shipped, with the video only if the build shipped it, and fails on
  any difference, and on any reference to a walkthrough file the build did not ship. That is what
  catches a stale cached render; the fix is the forced build above.

Only a leaf directive alone on its line embeds (`::walkthrough{#id}`, attributes allowed); a
directive quoted in code or inside an HTML comment, an inline `:walkthrough`, or a `:::` container
is not counted as using the declaration.
