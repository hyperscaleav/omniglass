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
  poster, the still that stands for the whole flow (the docs video's poster frame, and the PR
  comment's still below its animated preview); without it the last step is the poster.

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
inline on a PR or a docs page, and starting once the title card is painted (the recorder notes when
it was, so the preview never opens on the video's blank lead-in, and the budget gate fails one whose
opening frame is blank); `poster.png`; `step-NN.png` per step; and `walkthrough.json`, the
manifest of the title, the captions and the still each step produced (with the budget mark and
the preview's name added once the mp4 and the preview pass the budget). The preview's frame rate,
width and quality are set at the top of
[`web/e2e/walkthroughs.sh`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughs.sh).
The ids that failed are listed in `out/failed.txt`. Recording details, including the amd64-only
ffmpeg image, are in the [install guide](/guides/install/#recording-a-walkthrough).

## Show it on the PR

Push, then apply the **`run:walkthrough`** label. CI records the walkthroughs on the same stack and
keeps one PR comment that, for each walkthrough, links its full-quality video as **Watch
(scrubbable)**, which opens in the browser's own player and seeks, then shows its animated preview,
playing on the PR page, its poster, and its stills folded below; a failed walkthrough fails the job
and is named in the comment. The PR body's Visual confirmation links that comment on its
**Walkthrough** line.

**The evidence lives off main.** The video, preview, poster and stills are committed to the PR's
own ref, `refs/walkthroughs/pr-<n>`, never to the PR branch, and the comment reaches each file by
that commit's SHA. So the PR's head and its checks are untouched by the run (there is nothing to
pull and nothing to re-run), a squash-merge carries none of it into main, and a default clone
never fetches it (a clone fetches branches and tags, not other refs). The ref was chosen over an
orphan branch for that last reason: GitHub's raw host and jsDelivr serve a commit on any ref of a
public repository by its SHA, the workflow token can push the ref, and an orphan branch would sit
in every clone and in the branch list. Each run commits a fresh tree on top of the ref's last
commit, so an older comment's links keep resolving. The record job that runs the PR's code holds
only a read-only token; a separate publish job, which checks nothing out and runs nothing from the
PR, builds the evidence commit in a fresh repository and pushes it. The images are embedded from GitHub's raw
host; the video is linked through jsDelivr
(`https://cdn.jsdelivr.net/gh/<owner>/<repo>@<sha>/<id>/<id>.mp4`), because jsDelivr serves an mp4
as `video/mp4` with range requests, which is what lets a browser play and seek it, while the raw
host serves it as a download (GitHub plays no inline video a workflow can attach, so the preview
stays the inline glance). jsDelivr serves only public repositories, so this depends on the
repository staying public. What the label runs is described with the rest of the automation labels
in [labels](/contributing/labels/), and why it is hosted this way in
[ADR-0149](/architecture/decisions/#adr-0149-walkthrough-evidence-lives-on-per-pr-refs-its-pr-video-plays-through-jsdelivr-and-the-docs-commit-the-mp4).

**The evidence refs have a cost, and are pruned.** A ref keeps its commits, so every run adds its
files (about 2 to 3.5 MB per walkthrough, video included) to the repository's size on GitHub and to
any mirror that fetches every ref (`git clone --mirror`, a backup, a fork sync of all refs), though
not to a normal clone. `walkthrough-prune.yml` runs daily and deletes the evidence ref of every PR
closed more than 30 days ago; GitHub's garbage collection reclaims the objects later. After that,
the PR comment's images and video links stop resolving. Not deleting on close is deliberate: a
just-merged PR is still read.

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
so the page never restates a caption that could drift from the video. The embed renders the video
with controls, so a reader plays and scrubs it, with the poster step's still as its poster frame (a
still, so nothing moves for a reader whose system asks for reduced motion until they press play),
and that still again inside it for a browser that plays no video. The docs carry no animated
preview: a page that plays the video has no use for it, so the preview stays PR evidence. Below the
video come the title,
the description, a link to the mp4, and every step's caption over its still, folded under a disclosure. [Simulate a fleet](/guides/operator/simulation/#see-it) carries the first one.

The assets reach the site by a copy, committed like the screenshots:

```bash
make walkthroughs ID=sample-simulation   # record it
make docs-walkthroughs                   # copy what the docs declare into docs/public/walkthroughs/<id>/
```

`make docs-walkthroughs` copies, for every walkthrough a page declares, the manifest, the stills
and the video from `web/e2e/walkthroughs/out/<id>/` into `docs/public/walkthroughs/<id>/` (not the recorder's
`poster.png`, a copy of the poster step's still, nor the animated preview), removes any file left from an older recording,
and fails on a declared walkthrough that is not recorded, whose recording failed, or whose manifest
lacks the budget mark. Commit the copied files with the change.

**The docs build forces itself after a copy.** Astro caches each rendered page keyed on the page's
own content, and it has no way to learn that the embed also depends on the copied manifest, so a
plain build after a re-copy would serve the old render. That cache
also survives between deploys: a Cloudflare Pages build restores Astro's content cache (under the docs `node_modules`) from
the last one. So `corepack pnpm build` runs Astro through `docs/scripts/astro-build.mjs`, which
digests every `docs/public/walkthroughs/*/walkthrough.json`, the names of the files beside each one, and the
plugin sources that render the embed (`docs/scripts/lib/render-stamp.mjs`), compares the digest
with the stamp it wrote beside Astro's cache after the last good build, and runs
`astro build --force` only when they differ. Nothing to remember after a copy; only a bare
`astro build` skips the stamp, and the post-build check below fails that build if it renders stale.

**The video is committed, and that is a cost.** Git keeps every version of a binary, so each
re-copy of a walkthrough adds its new mp4 (about 0.6 to 1.2 MB today) to the history beside its new
stills, and that size never leaves the repository: roughly 1 MB per walkthrough per re-record, about
6 MB for the six the docs carry today. The trade is made on purpose: the video is what lets a reader
of the deployed docs, and of a docs PR preview, play and scrub the flow, which no uncommitted file
can do on a static site. Earlier copies committed an animated preview beside the stills; those
versions stay in the history, but the docs no longer carry one. Re-record and
re-copy when a flow changes, not to refresh one that has not. A PR's own walkthrough evidence costs
the repository nothing on main: it lives on the PR's evidence ref (above).

Three checks keep the embed true to its recording:

- **Before the build**, `docs/scripts/check-embeds.mjs` fails a `::walkthrough` with no id, an id
  its page does not declare, or a declared walkthrough with no copied manifest.
- **In `make test`**, the docs lint suite (`internal/docslint/walkthroughs.go`) fails a declared
  id with no script under `web/e2e/walkthroughs/`, a declaration no directive on its page uses, a
  copied manifest with no budget mark, a missing still, a poster that is none of the stills, a
  missing video, a `preview.webp` in a copy (the docs carry none), a preview name in the manifest
  that is not `preview.webp`, a copy
  that has drifted from its script (title, description, step count or any caption), and a copied
  directory no page declares.
- **After the build**, `docs/scripts/check-walkthrough-embeds.mjs` re-renders every built embed
  from the manifest the build shipped and fails on any difference, on an embed whose video the
  build did not ship, and on any reference to a walkthrough file the build did not ship. Through `pnpm build` the
  stamp above keeps it from firing for staleness; it is the backstop for any build that skips it.

Only a leaf directive alone on its line embeds (`::walkthrough{#id}`, attributes allowed); a
directive quoted in code or inside an HTML comment, an inline `:walkthrough`, or a `:::` container
is not counted as using the declaration.
