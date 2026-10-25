---
title: Demos
description: "A demo is a recording of a PR's feature working, made against the real stack: a terminal demo (a VHS tape) for CLI and API work, a walkthrough (a Playwright script) for a console flow. When a PR needs one, how to write, record and review each, and how a docs page embeds a walkthrough."
---

A **demo** is a short recording of a pull request's feature working, made against the
[walkthrough stack](/guides/install/#the-whole-loop-in-docker) (a server, a collecting node and a
simulator, all in Docker) and posted on the PR, so a reviewer watches the feature work rather than
reading a transcript of it. A demo is one concept with two recorders, one for each way the product
is driven:

- A **terminal demo** shows CLI and API work. It is a [VHS](https://github.com/charmbracelet/vhs)
  tape, `scripts/demo/<id>.tape`, typed into a real terminal by the CLI the PR builds, and written
  out as a GIF (what plays inline on the PR) and an MP4 (what a reader scrubs).
- A **walkthrough** shows a console flow. It is a script, `web/e2e/walkthroughs/<id>.yaml`, played
  by a Playwright recorder in the real console, with a caption on every step, and written out as an
  mp4, an animated preview, a poster frame and one still per step.

A demo id names one demo of either kind, its recording directory and its files; a tape and a
walkthrough never share one (`make test-web` refuses it). Both kinds share one label, one CI job,
one PR comment and one place the evidence lives, below.

A demo is also a test. A terminal demo fails when a command it types exits non-zero or a wait in it
runs out; a walkthrough fails when a step's result never appears. Either fails when its recording is
over budget. A failing demo fails the run, naming the demo and the cause.

## When a PR needs one

**Every PR demos its feature, live.** Decide which from the diff, not from intent:

- **CLI or API behavior** (a new or changed route, command, flag or answer) takes a terminal demo
  that drives it through the CLI.
- **An operator flow in the console** takes a walkthrough: a form, a multi-step path, a drawer
  action, a route, or the steps between them, anything an operator now does in a different order or
  with different input. A visual-only change (a restyle, a new column, a moved badge) keeps
  [screenshots](/contributing/docs-with-everything/#screenshots-are-generated-not-pasted) alone.
- A PR with both takes both.
- **n/a only with a reason**, in one clause: a test-only change, a docs-only change, a refactor
  with no behavior an operator or a client can observe.

The `/ship-slice` gate holds the PR body's **Demo** section to this, exactly as it holds a UI PR to
its screenshots: a PR that needs a demo is not `ready` without the demo comment
([the slice workflow](/contributing/slice-workflow/)).

## Write a terminal demo

A terminal demo is `scripts/demo/<id>.tape`, in VHS's own format: one command per line (`Type` a
command, `Enter`, `Wait` for the prompt to come back, `Sleep`, `Hide` and `Show`). It opens with a
comment block: the first line is the title, the rest the description, and both head the demo in the
PR comment. The house rules (which commands a tape may use, that every `Enter` is waited on, no uuid
in typed text, `Hide` and `Show` in pairs) and an annotated example are defined once, in the header
of [`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs),
the module the recorder and the unit tests both run. This page does not restate them.

A tape sets nothing about how it looks. The recorder wraps it in the **house settings** (one
terminal size, font, theme and typing speed, `HOUSE` in the same module) and its outputs, so every
demo reads alike and a re-recording differs only where the product did; a `Set`, `Output` or a
per-command typing speed in a tape is refused.

Inside the terminal, the CLI is already pointed at the stack's server with an owner token, and a
prelude, sourced unseen before the first frame
([`web/e2e/tape/prelude.sh`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tape/prelude.sh)),
makes the shell a test and gives a tape two helpers. Write it the way a reviewer will watch it:

- **One feature, the shortest honest path.** Type the real commands an operator or an integrator
  would, in the order they would.
- **Narrate with comments.** A typed `# ...` line is a caption the shell ignores: say what the next
  commands show, in the product's vocabulary.
- **Address by name.** A component by its name or dotted address
  (`hq.west.level-2.huddle.$comp.panel-1`), never a uuid; the rules refuse a uuid in typed text.
  Where a route still takes an id, look it up by name inside the tape and type the variable.
- **Show only what holds still.** An id, a timestamp, a count of requests or the stack's own address
  changes from run to run; pick the stable fields with `jq` (`jq -c '{name, driver}'`), so a
  re-recording shows only what changed in the product.
- **Hide the waits.** The fleet answers on its own schedule. In a `Hide` block,
  `demo_until <seconds> '<code>'` retries code until it succeeds (and fails the demo if it never
  does), and `demo_hush '<code>'` runs a lookup unseen; each erases its own line, so the screen
  shows nothing of the block when `Show` resumes. A wait never costs the GIF any frames.
- **Read the stack's logs.** The server's, the node's and the simulator's logs are files
  (`/stack/logs/server.log`, `/stack/logs/node.log`, `/stack/logs/sim.log`): grep them for what
  the API does not show, keeping to the stable part of each line.
- **Expect a failure out loud.** A command the tape types that exits non-zero fails the demo. One
  that is meant to fail says so on screen, with `|| true`.

`make test-web` holds every tape in the tree to the rules, so a malformed one fails before a stack
starts. [`scripts/demo/http-logins.tape`](https://github.com/hyperscaleav/omniglass/blob/main/scripts/demo/http-logins.tape)
is the first one.

## Write a walkthrough

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

Every recording has a ceiling, and the limits are defined once beside each recorder's rules, not
restated here. A terminal demo is held to `MAX_GIF_BYTES` (its GIF plays inline on the PR page) and
`MAX_SECONDS` in
[`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs);
its hidden waits cost nothing, so what counts is typing and answers. A walkthrough is held to
`MAX_SECONDS`, `MAX_BYTES` and `MAX_PREVIEW_BYTES` in
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs),
and a recording with no preview fails too. A recording at or over a limit fails, naming which, and
only one that passed carries the budget mark in its manifest (`demo.json`, `walkthrough.json`),
which is what the PR comment, and the docs copy of a walkthrough, refuse to go without. A demo that
will not fit is two demos: split it.

## Record it locally

With nothing but Docker:

```bash
make demo ID=http-logins   # one demo of either kind (ID="a b" for several, no ID for every demo)
make test-demos            # the terminal recorder's own test: a failing command, a wait that runs out, a stuck Wait, the budget
make test-walkthroughs     # the walkthrough recorder's own test: the failure and budget paths
```

`make demo` records each demo it names with its own recorder, each on a walkthrough stack of its
own, and exits non-zero if any failed; each recorder lists the ids that failed in its own
`failed.txt`. (`make walkthroughs` still records walkthroughs alone.)

**A terminal demo** is played by VHS from its own image, pinned by digest, which bundles what VHS
drives (ttyd for the terminal, chromium to render it, ffmpeg to encode) and the fonts the house
settings name; the recorder adds `jq`, the prelude and this tree's `omniglass` binary
([`web/e2e/tape/Dockerfile`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tape/Dockerfile)),
so there is nothing to install, and a recording renders the same on a laptop and in CI. It writes
`scripts/demo/out/<id>/`: `<id>.gif`, `<id>.mp4`, and `demo.json`, the manifest with the title, the
description and the budget mark; beside them, the tape as it was played (wrapped in the house
settings), the shell's unseen output (`shell.log`), and any command that failed
(`failed-commands.txt`). The stack's container logs are kept under the output directory too.

**A walkthrough** is played in a Playwright container with a visible cursor, a highlight on each
target and a caption bar, and written to `web/e2e/walkthroughs/out/<id>/`: `<id>.mp4`;
`preview.webp`, an animated preview encoded from the mp4 at a lower frame rate and width that loops
forever, small enough to load inline on a PR or a docs page, and starting once the title card is
painted (the recorder notes when it was, so the preview never opens on the video's blank lead-in,
and the budget gate fails one whose opening frame is blank); `poster.png`; `step-NN.png` per step;
and `walkthrough.json`, the manifest of the title, the captions and the still each step produced
(with the budget mark and the preview's name added once the mp4 and the preview pass the budget).
The preview's frame rate, width and quality are set at the top of
[`web/e2e/walkthroughs.sh`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughs.sh).
Recording details for both, including the image each recorder pins, are in the
[install guide](/guides/install/#recording-a-demo).

## Show it on the PR

Push, then apply the **`run:demo`** label (its earlier name, `run:walkthrough`, still works). CI
records the PR's demos: the tapes and walkthrough scripts it adds or changes; every tape when it
changes the terminal recorder, and every walkthrough when it changes anything else under `web/`,
since any console change can move a flow; every demo when it names none. It keeps **one PR
comment** listing every demo it recorded: a terminal demo's GIF, playing on the PR page, with its
MP4 linked as **Watch (scrubbable)**; a walkthrough's video, linked the same way, then its animated
preview, its poster and its stills folded below. A failed demo fails the job and is named in the
comment. The PR body's **Demo** section links that comment. A PR labelled before demos keeps its
walkthrough comment, updated in place.

**The evidence lives off main.** Every file is committed to the PR's own ref,
`refs/walkthroughs/pr-<n>` (named when walkthroughs were the only demos), never to the PR branch,
and the comment reaches each file by that commit's SHA. So the PR's head and its checks are
untouched by the run (there is nothing to pull and nothing to re-run), a squash-merge carries none
of it into main, and a default clone never fetches it (a clone fetches branches and tags, not other
refs). The ref was chosen over an orphan branch for that last reason: GitHub's raw host and
jsDelivr serve a commit on any ref of a public repository by its SHA, the workflow token can push
the ref, and an orphan branch would sit in every clone and in the branch list. Each run commits a
fresh tree on top of the ref's last commit, so an older comment's links keep resolving. The record
job that runs the PR's code (both recorders, and every command a tape types) holds only a read-only
token; a separate publish job, which checks nothing out and runs nothing from the PR, builds the
evidence commit in a fresh repository and pushes it. The images (a GIF, a preview, a still) are
embedded from GitHub's raw host; every video is linked through jsDelivr
(`https://cdn.jsdelivr.net/gh/<owner>/<repo>@<sha>/<id>/<id>.mp4`), because jsDelivr serves an mp4
as `video/mp4` with range requests, which is what lets a browser play and seek it, while the raw
host serves it as a download (GitHub plays no inline video a workflow can attach, so the GIF and the
preview stay the inline glance). jsDelivr serves only public repositories, so this depends on the
repository staying public. What the label runs is described with the rest of the automation labels
in [labels](/contributing/labels/); why it is hosted this way is
[ADR-0149](/architecture/decisions/#adr-0149-walkthrough-evidence-lives-on-per-pr-refs-its-pr-video-plays-through-jsdelivr-and-the-docs-commit-the-mp4),
and why both recorders share it is
[ADR-1132](/architecture/decisions/#adr-1132-a-demo-is-one-concept-with-two-recorders-and-every-pr-demos-its-feature).

**The evidence refs have a cost, and are pruned.** A ref keeps its commits, so every run adds its
files (a terminal demo's GIF and MP4 are a few MB together; a walkthrough is about 2 to 3.5 MB,
video included) to the repository's size on GitHub and to any mirror that fetches every ref
(`git clone --mirror`, a backup, a fork sync of all refs), though not to a normal clone.
`demo-prune.yml` runs daily and deletes the evidence ref of every PR closed more than 30 days ago;
GitHub's garbage collection reclaims the objects later. After that, the PR comment's images and
video links stop resolving. Not deleting on close is deliberate: a just-merged PR is still read.

## Review a demo

Watch it the way the author's reviewer will. For a terminal demo, check that each command is one an
operator or an integrator would type, that each answer shows what its narration promises, and that
nothing on screen varies from run to run (an id, a time, an address). For a walkthrough, a reviewer
who cannot watch the video, an agent above all, grades the per-step stills instead: open each
`step-NN.png` in order beside its caption and check that each step lands on the result its caption
promises, that the cursor and highlight sit on what the caption names, that the caption bar hides
nothing the step is about, and that the poster is the moment the flow builds to. The full rubric is
the walkthrough section of the
[`/assess-ui` skill](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/assess-ui).
An agent reviewing a terminal demo reads the tape beside frames ffmpeg pulls from its MP4. A failing
line is a fix to the feature or the demo, then a re-record and a second look.

## Embed a walkthrough in the docs

A docs page embeds a walkthrough the way it embeds a screenshot: it declares the id in
`walkthroughs` frontmatter and places it with a directive. (A terminal demo is PR evidence only, for
now.)

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
re-copy when a flow changes, not to refresh one that has not. A PR's own demo evidence costs
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
