---
title: Demos
description: "A demo is a recording of a PR's feature working, made on the lab (omniglass lab run): a terminal demo (a VHS tape) for CLI and API work, a walkthrough (a Playwright script) for a console flow. When a PR needs one, how a PR tags it, how to write, record and review each, and how a docs page embeds a walkthrough."
---

A **demo** is a short recording of a pull request's feature working, made on the
[lab](/developer/lab/) (the server, and an outpost collecting from the simulator in its own process, all in
Docker: the same lab a learner runs, "lab instance" banner and all) and posted on the PR, so a reviewer watches the feature work rather than
reading a transcript of it. A demo is one concept with two recorders, one for each way the product
is driven:

- A **terminal demo** shows CLI and API work. It is a [VHS](https://github.com/charmbracelet/vhs)
  tape, `scripts/demo/<id>.tape`, typed into a real terminal by the CLI the PR builds, and written
  out as a GIF (what plays inline on the PR) and an MP4 (what a reader scrubs).
- A **walkthrough** shows a console flow. It is a script, `web/e2e/walkthroughs/<id>.yaml`, played
  by a Playwright recorder in the real console, with a caption on every step, and written out as an
  mp4, an animated preview, a poster frame and one still per step.

A demo id names one demo of either kind, its recording directory and its files; a tape and a
walkthrough never share one (`make test-web` refuses it). Both kinds share one tag, one label, one
CI workflow, one PR comment and one place the evidence lives, below.

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

**Tag it in the PR body.** The Demo section carries a line of its own naming the demo or demos the
PR is about, by id:

```markdown
demo: http-logins
demo: http-logins issue-a-command
demo: none (docs-only change)
```

The tag is what `run:demo` records (below), so it is required: `demo-tag.yml` fails a PR whose body
has no tag, a malformed one, or one naming a demo its tree does not have, and `/ship-slice` holds
the PR to a tag exactly as it holds a UI PR to its screenshots
([the slice workflow](/contributing/slice-workflow/)). `none` takes its reason in parentheses. The
tag is read from the body's text only, so one quoted in a code block or an HTML comment (as the PR
template's own instructions are) is not the PR's.

## Write a terminal demo

A terminal demo is `scripts/demo/<id>.tape`, in VHS's own format: one command per line (`Type` a
command, `Enter`, `Wait` for the prompt to come back, `Hide` and `Show`). It opens with a
comment block: the first line is the title, the rest the description, and both head the demo in the
PR comment. The house rules (which commands a tape may use, that every `Enter` is waited on, no uuid
in typed text, `Hide` and `Show` in pairs) and an annotated example are defined once, in the header
of [`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs),
the module the recorder and the unit tests both run. This page does not restate them.

A tape sets nothing about how it looks. The recorder wraps it in the **house settings** (one
terminal size, font, theme and typing speed, `HOUSE` in the same module) and its outputs, so every
demo reads alike and a re-recording differs only where the product did; a `Set`, `Output` or a
per-command typing speed in a tape is refused.

**A tape is paced, not slept.** Its GIF plays inline on the PR, where a reader cannot pause it, so
after every line a tape shows, the shell holds the screen before its prompt comes back (and so
before the tape's `Wait` lets the next command type): a base read time of a few seconds, more for
each row the command's output filled, up to a cap; a typed `# caption` holds the same way by its
words. One rule paces every tape. Its numbers are `PACE` in
[`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs),
and the prelude applies them
([`web/e2e/tape/pace.sh`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tape/pace.sh)),
counting the rows from what the terminal was actually sent. So a tape writes no `Sleep` (the rules
refuse one, since it would stack on the rule's hold or stand in for it), and it types nothing that
reaches the rule's hooks: the prompt variables, the prelude's own names, an `exec`, a `trap` or a
new interactive shell. An unseen `demo_until` or `demo_hush` and a `clear` are not held. Each hold
the rule gave is listed in the recording's `pace.txt`, one line each: the milliseconds, `output` and
the rows or `caption` and the words, and the line typed.

Inside the terminal, the CLI is already pointed at the lab's server with an owner token, and a
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
- **Show only what holds still.** An id, a timestamp, a count of requests or the lab's own address
  changes from run to run; pick the stable fields with `jq` (`jq -c '{name, driver}'`), so a
  re-recording shows only what changed in the product.
- **Hide the waits.** The fleet answers on its own schedule. In a `Hide` block,
  `demo_until <seconds> '<code>'` retries code until it succeeds (and fails the demo if it never
  does), and `demo_hush '<code>'` runs a lookup unseen; each erases its own line, so the screen
  shows nothing of the block when `Show` resumes. A wait never costs the GIF any frames.
- **Answer a command's own question.** A command that asks (a `[y/N]` before it runs a step) is
  answered after a `Wait+Screen /<the question>/`, with a `Type` and an `Enter`, and still ends on
  a plain `Wait`. The pattern has to be one that nothing typed since the last prompt matches, or
  it could fire on the command line itself; the rules refuse the answer otherwise
  (`scripts/demo/omnibot.tape` answers two).
- **Read the lab's logs.** The server's and the outpost's logs are files (`/stack/logs/server.log`,
  `/stack/logs/outpost.log`; the outpost's carries its in-process simulator's lines too): grep them for
  what the API does not show, keeping to the stable part of each line.
- **Expect a failure out loud.** A command the tape types that exits non-zero fails the demo. One
  that is meant to fail says so on screen, with `|| true`.

**A tape that needs a database says so.** A behavior that depends on what the database holds (a
server refusing a schema a newer release migrated) cannot be staged through the API, so the tape
declares a `# demo-needs: database` comment line, anywhere in the file (the header is read by
`NEEDS` in the same module, and an unknown need is refused naming the line). For such a tape the
recorder makes a database of its own on the lab's Postgres (`tape_<id>`, hyphens as underscores)
before it plays, hands the tape `OMNIGLASS_DSN` pointing at it, so the direct-database commands
(`omniglass migrate`, `omniglass server`) run against it, and drops it when the tape ends, passed or
failed. The recorder image carries `psql` (the lab's Postgres client, pinned by digest at the lab's
major) to stage a row in it. It is never the lab's own database: a tape may stamp a schema no
later tape could migrate. A tape that declares nothing is handed no DSN. A tape that needs the
Docker socket (`# demo-needs: docker-socket`, opt-in as above) does not combine with `database`,
since it runs on the host's network, which cannot reach the lab's Postgres.
[`scripts/demo/schema-ahead.tape`](https://github.com/hyperscaleav/omniglass/blob/main/scripts/demo/schema-ahead.tape)
is the first: it migrates its database, stamps a version past this build's head, and shows the
server refusing it (#1289).

`make test-web` holds every tape in the tree to the rules, so a malformed one fails before a lab
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
- **End every action on its result.** After a click, a hover, a typed value or a key press, wait for what
  the console shows in answer (the drawer open, the tooltip shown, the row added, the status changed). That wait is
  the assertion that makes the walkthrough a test.
- **Captions teach.** Say why the operator takes the step, in the product's vocabulary
  (component, location, system), short enough to read in the caption bar.
- **Mark the payoff.** `poster: true` on the step the walkthrough builds to makes its still the
  poster, the still that stands for the whole flow (the docs video's poster frame, and the PR
  comment's still below its animated preview); without it the last step is the poster.
- **Stage what the flow does not teach in `setup`.** A walkthrough plays on the lab's dev fleet. When
  its scene needs one more thing the operator does not make on camera (set-up-monitoring's display
  in Bay 2 that nothing collects from yet), a `setup` list makes it through the API before the
  title card, with the lab's owner token; one that already exists counts as made, so a replay on a
  lab the walkthrough already ran on works. Keep it to the scene: the flow itself stays on camera.
- **Stage a moment, not only a thing.** A scene that needs something to have happened (an alarm
  raised and cleared, as read-what-happened-on-the-timeline needs) puts a state with `put` (a
  simulated device's impairment), waits with `until: { get, contains, timeout }` until a read
  holds a text (the alarm list holding `raised_at`), and lets time pass with `pause: <seconds>`
  (1 to 300), all off camera before the title card. Leave the lab as you found it (unplug, then
  replug), since the probe replays every walkthrough on one stack. The lab facts list only the
  creates; a learner trips the same fault on the Simulation page.
- **Give your setup a component and alarms of its own.** The recorder plays each walkthrough on a lab
  of its own, but [the probe](/contributing/synthetic-user/#the-probe-and-its-digest) replays all of
  them, in alphabetical order, on one shared stack, where that "already exists counts as made" rule
  hands one walkthrough the state another left behind. Two walkthroughs whose setups create the same
  component, or raise the same alarm (the component and the dedup key), and whose steps click, type,
  select or press anything, fail `make test-web` (`setupCollisions` in `web/e2e/walkthroughlib.mjs`).
  Two alarms on one component are fine, but then a step names its own: give the alarm a message no
  other walkthrough's alarm shares, and target the control by the name the console gives it for that
  alarm (the leaf's buttons read "Snooze" followed by the alarm's message), never by a bare "Snooze".
- **Wait for a filling console off camera with `ready`.** When the scene is only there once the lab
  has run for a while (read-platform-health's Requests panel fills after the server's first report
  window), a `ready` block (`goto` a console path, `wait` for a target, with its own `timeout` up to
  three minutes) is waited for after sign-in and before the title card, so the wait never spends
  the recording's budget. A ready that never comes fails the run naming what it waited for.

- **Name what the lab must run with `lab`.** A walkthrough plays on the dev fleet. One that signs in
  through an identity provider (sign-in-with-sso) says `lab: [sso]`, and the recorder's lab then
  runs Dex beside it, with single sign-on configured against it and a demo person, Avery Quinn,
  whose identity there is linked (the demo values are in `internal/lab/sso.go`). The provider's
  address is on the lab network, which the recorder's browser joins and yours does not, so only a
  script's lab runs it, and the [lab guide](/developer/lab/) lists that walkthrough as recorded
  rather than followed. The probe runs it too, and plays such a walkthrough in a browser context of
  its own, so the walkthroughs after it still play as the owner.

`make test-web` validates every script in the tree, so a malformed one fails before a browser
starts.

## The budget

Every recording has a ceiling, and the limits are defined once beside each recorder's rules, not
restated here. A terminal demo is held to `MAX_GIF_BYTES` (its GIF plays inline on the PR page) and
`MAX_SECONDS` in
[`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs);
its hidden waits cost nothing, so what counts is typing, answers and the pacing rule's holds.
`make test-web` adds up the least each tape can run (its typing on screen and the rule's least
hold after each line) and fails one already over `MAX_SECONDS`, before a lab starts. A walkthrough is held to
`MAX_SECONDS`, `MAX_BYTES` and `MAX_PREVIEW_BYTES` in
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs),
and a recording with no preview fails too. A recording at or over a limit fails, naming which, and
only one that passed carries the budget mark in its manifest (`demo.json`, `walkthrough.json`),
which is what the PR comment, and the docs copy of a walkthrough, refuse to go without. A demo that
will not fit is two demos: split it.

A walkthrough's animated preview also has to open on the painted title card, not the blank or
half-painted lead-in the video starts with. The opening frame is chosen from the recording, not by a
fixed beat after the paint: the recorder notes when the card was painted, and the preview opens on
the first frame at or after that, within the card's hold, whose brightest luma
(`signalstats` YMAX) reaches `PREVIEW_MIN_YMAX` (`previewStartFromLumas`). A loaded runner whose
video lags the page only moves the start later. The gate fails only when no frame in that window
qualifies, which means the card never painted, and it names the window scanned and the brightest
luma it found.

## Record it locally

With nothing but Docker:

```bash
make demo ID=http-logins   # one demo of either kind (ID="a b" for several, no ID for every demo)
make test-demos            # the terminal recorder's own test: the pacing, a failing command, a wait that runs out, a stuck Wait, the budget
make test-walkthroughs     # the walkthrough recorder's own test: the failure and budget paths
```

`make demo` builds (`make`), then records each demo it names with its own recorder, each on a lab of
its own, and exits non-zero if any failed; each recorder lists the ids that failed in its own
`failed.txt`. (`make walkthroughs` still records walkthroughs alone.) A recorder's lab is the
product's own (`./bin/omniglass lab run`, run by
[`web/e2e/lib/lab.sh`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/lib/lab.sh)) as a
throwaway: a project of its own per run, so two recordings on one Docker daemon never meet, on a port
Docker picks, held for the recorder's process, and torn down with its data on every exit path (a
recorder killed too hard to stop it leaves the lab to notice and tear itself down). The recorder's
browser or terminal joins the lab's network and reaches the server there. A lab disturbed from
outside mid-recording (a container stopped or removed) fails the run saying so, never as a test
failure or UI drift.

The browser e2e tier (`make test-e2e`) and the docs screenshots (`make docs-shots`,
`make docs-shots-check`) ride the same throwaway lab, through the same `lab.sh` (#1267), so the repo
stands omniglass up in Docker one way. Each fits the lab to what it checks: the e2e tier's is
seeded with the `empty` scenario, so its CRUD round-trips start from a fresh install, and the
screenshots' runs with `--no-outpost`, the dev fleet without the simulated wiring and with no outpost
collecting from it, so a page photographed twice is the same page, and with `--platform-history`, an
hour of made-up server figures, so platform health's sparklines draw full lines (#1790). The lab's banner is a fact about
the lab rather than the product the docs teach, so the screenshot capture hides it.

**A terminal demo** is played by VHS from its own image, pinned by digest, which bundles what VHS
drives (ttyd for the terminal, chromium to render it, ffmpeg to encode) and the fonts the house
settings name; the recorder adds `jq` (copied from jq's own image, also pinned by digest), the
prelude and this tree's `omniglass` binary
([`web/e2e/tape/Dockerfile`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tape/Dockerfile)),
so there is nothing to install, and a recording renders the same on a laptop and in CI. The
recorder pulls those base images while the lab comes up, so the VHS image (about 0.9 GB to pull)
costs a fresh CI runner no time of its own, and a machine that has pulled it once pulls nothing. It writes
`scripts/demo/out/<id>/`: `<id>.gif`, `<id>.mp4`, and `demo.json`, the manifest with the title, the
description and the budget mark; beside them, the tape as it was played (wrapped in the house
settings), the shell's unseen output (`shell.log`), the hold the pacing rule gave each line
(`pace.txt`), and any command that failed (`failed-commands.txt`). The lab's container logs are kept under the output directory too.

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
Recording details for both, including the image each recorder pins, are in
[recording details](#recording-details).

## Show it on the PR

Tag the demo in the PR body (above), push, then apply the **`run:demo`** label (its earlier name,
`run:walkthrough`, still works, on a branch that has merged main since this selection landed). CI
records exactly the demos the PR is about:

- the demos its tag names;
- any tape or walkthrough script the PR adds or changes, tagged or not (a demo is a test, so a
  changed one is played).

Nothing else. Touching the console, a recorder, the lab or the pipeline records no demo the PR
did not name: a change that could break a demo it never named is caught by the nightly freshness
run (below), not by recording every demo on every PR, which took 20 minutes or more a run. A PR
whose tag is `demo: none (<reason>)` and that changes no demo records nothing; a PR with no tag
fails the run, saying how to tag. To prove a change to a recorder or the lab, tag a demo of each
kind it touches.

Each selected demo records on a runner and a lab of its own, in parallel, so a run takes as long
as its slowest demo (about five to seven minutes) rather than the sum, and the run's summary says
which demos it chose and why. The images are built once: the select job pulls the lab's Postgres
(from Google's Docker Hub mirror, retried, and from Docker Hub only if the mirror cannot serve it) and builds
the omniglass image and the lab binary, then saves them as one artifact; each record job downloads it and
loads it, so no record job pulls Postgres or builds the omniglass image. That matters because the
nightly run starts about a hundred record jobs at once, and Docker Hub counts anonymous pulls per shared
runner address (HTTP 429, #2020). A lab that still has to pull retries a 429 with backoff, and a pull
still refused fails its bring-up with an error naming the rate limit; the recorder leaves an `infra` mark, and
the run:demo comment, the clip and the freshness report list that demo as an **infrastructure failure**, apart from a
failed demo and from one that did not run. One job then
combines every demo into one result:

- **The clip in the PR body.** Each recorded demo's clip, its GIF (a terminal demo) or animated
  preview (a walkthrough) with its **Watch (scrubbable)** link, is written into the PR body's Demo
  section, between two HTML-comment markers, so the PR carries its video where a reviewer starts
  reading. A re-run replaces only that block; the author's text around it is never touched. A
  marker counts only on a line of its own outside a code block, so a body that quotes one keeps
  its text. With no block yet, it goes after the tag line (or at the end of a body with no tag). A
  body whose marker lines are unpaired or doubled is left alone and the job fails, saying so. The
  run reads the body as it is when it starts, so re-running an older run records what the tag
  says now.
- **The demo comment, the full record.** **One PR comment** lists every demo: a terminal demo's GIF
  with its MP4 linked as **Watch (scrubbable)**; a walkthrough's video linked the same way, then its
  animated preview, its poster and its stills folded below; the run and its artifacts; and any demo
  that failed or never ran (a demo whose lab never came up is named as that, not as a failure).
  The clip links it. A failed demo fails its job. A PR labelled before demos keeps its walkthrough
  comment, updated in place.

**The freshness run.** Since a PR records only what it names, the same workflow re-records every
demo on `main` each night (or when run by hand from the Actions tab), each on its own runner. Its
recordings stay in the run's artifacts for 14 days: main needs no comment, and committing them to
an evidence ref every night would grow the repository for nothing. When a demo fails or never runs,
the run opens one issue (or updates the open one, with a comment naming the run), and the next
green run closes it.

**The evidence lives off main.** Every file is committed to the PR's own ref,
`refs/walkthroughs/pr-<n>` (named when walkthroughs were the only demos), never to the PR branch,
and the comment reaches each file by that commit's SHA. So the PR's head and its checks are
untouched by the run (there is nothing to pull and nothing to re-run), a squash-merge carries none
of it into main, and a default clone never fetches it (a clone fetches branches and tags, not other
refs). The ref was chosen over an orphan branch for that last reason: GitHub's raw host and
jsDelivr serve a commit on any ref of a public repository by its SHA, the workflow token can push
the ref, and an orphan branch would sit in every clone and in the branch list. Each run commits a
fresh tree on top of the ref's last commit, so an older comment's links keep resolving. A run in
which no demo recorded commits an empty tree, so its comment and clip still say what happened. The record
jobs that run the PR's code (both recorders, every command a tape types, and the script that
stages the evidence, the comment and the clip) hold only a read-only token; a separate publish job,
which checks nothing out and runs nothing from the PR, builds the evidence commit in a fresh
repository and pushes it, upserts the comment, and splices the clip into the PR body. The
freshness run's issue is filed the same way, by a job that holds only the issues token and runs
nothing from the tree. `web/src/demo-workflow.test.ts` holds both boundaries against the workflow
itself. The images (a GIF, a preview, a still) are
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
An agent reviewing a terminal demo reads the tape beside frames ffmpeg pulls from its MP4, one a
second, and checks that every answer stays on screen, whole and unscrolled, for the frames its
`pace.txt` line promises, before the next command starts typing. A failing
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
the description, a link to the mp4, and every step's caption over its still, folded under a disclosure. [Simulate a fleet](/operator/simulation/#see-it) carries the first one.

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
the last one. So `pnpm build` runs Astro through `docs/scripts/astro-build.mjs`, which
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

## Marketing captures

The stills and loops on the hyperscaleav.com Omniglass page are recorded from a real corporate lab, not
mocked, so a UI change is one command away from a reshoot (#2299). `make marketing-shots
MARKETING_DEST=<dir>` brings up a corporate lab (`omniglass lab run --scenario corporate --systems 1000`,
playing the `corporate-outage` omnibot recording, so no capture does live inference), plays every shot in
[`web/e2e/marketing/shots.yaml`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/marketing/shots.yaml),
and writes the files and a `shots.json` manifest into the directory. The landing repo points it at
`public/images/omniglass/shots/` and reads only the manifest, so a reshoot that keeps the shot ids updates
every image, loop, poster and alt text with no code change there.

A shot is **data**: an id, a kind, a viewport, an `alt`, setup, and steps. A `still` is one 2x WebP. A
`loop` is an H.264 MP4 of 5 to 9 seconds (a loop's `max_seconds` may raise that to 30, for a story such as
omnibot's answer that cannot be told faster without faking it, or the page's teaser) and at most 1.5 MB (a
loop allowed past 15 seconds earns that rate for its length, so a 30 second one may weigh 3 MB), with a WebP poster; the recorder re-encodes
a loop smaller until it fits, and a loop that cannot fit, or is too short or too long, fails the run
naming the shot and the limit.

A loop's length is two budgets, because only part of it is the shot list's. What the steps **spend**
(each `hold`, the recorder's pace: a 250 ms glide before a pointer acts, 35 ms a typed key, 350 ms after a
page loads, each `lapse`, the 300 ms settle at the end and the camera's last ease out) is held to the
maximum exactly, when the list is parsed, so a shot that cannot fit fails before any lab starts
(`authoredSeconds`). Everything else is the lab's: how long a click takes to answer and a wait takes to
see its target, which a 1,000-system lab on a busy host stretches. The recording may run 15% past the
maximum for that (`LOOP_TOLERANCE`), and a loop past its maximum at all prints the steps the lab
stretched most, beside what the list spends on each, so the fix (a `lapse` on a slow wait, a shorter
hold) is plain rather than a raised maximum (`overrunReport`). A wait on the lab's recorded omnibot spends
the recording's own `think` pauses, which the parse does not count, so the report shows them as the
lab's; omnibot-ask's maximum leaves room for them. The recorder copies nothing to the destination until
every shot has encoded within budget, so a failed run publishes nothing. Steps use the walkthrough grammar (`goto`, `click`, `hover`, `type`,
`select`, `press`, `wait`, and `hold` to linger), plus `call`, with no captions: a capture speaks for itself, and the
page that hosts it writes the words. A step that never finds its target fails the run, naming the shot,
the step and what it waited for, and keeps the frame it failed on.

A loop has a camera. A step's `focus` moves it: `here` frames the step's own target, a target of its own
(`text`, `role`, `selector`, as a step names one) frames that, and `out` returns to the whole frame. `zoom`
(1 to 3) sets how close; without it the camera fits the target with a margin, never past 3x. A loop is
watched on a phone too, where the console's 13-pixel text needs 2x or more to read. The camera
eases over 0.6 s and centres the target, even near the capture's edge, where the frame shows the console's
own dark base past it; so focus the thing the beat is about, not the panel around it. A target bigger than
the frame at its `zoom` (a whole card) keeps its top and left edges in view instead, so its heading shows. It eases back to the whole frame before the loop
ends, so the loop's seam lands on the full view: the recorder keeps recording for that last ease when the
final step leaves the camera zoomed. A focus on a still, or on a loop's prep, fails the parse.

A focus opens a **beat**, which runs until the next focus or the clip's end. A step's `expect` lists the
text its beat's frame must show: once that step has played (its `hold` included), the recorder reads the text on screen inside the frame
(each text node wholly inside it, and not covered by a backdrop or dialog) and checks every string. A
miss does not stop the run: every shot is still written, and the run fails at its end, naming the shot,
the step, what was missing and what the frame held. Each loop also gets a **storyboard**, one still per
beat taken from the finished clip at that moment, in the storyboard folder of the run's output, with each
beat's in-frame text in `<id>.beats.json` beside the clip. A loop's beats (when each opens, is checked and ends, and
the part of the capture it frames) ride along in `shots.json`. Write an `expect` from the words the
page's bullet rests on, as the console renders them (an input's value is not text on screen).
The recorder takes the loop's frames as screenshots at twice the viewport, as fast as the page allows
(Playwright's own video has CSS-pixel frames, so a "2x video" is the 1x frame padded with gray), so a 2x zoom
is still crisp (past 2x the frame is upscaled, a little soft), and writes where each focused target sat on screen. ffmpeg joins the frames, each held for
as long as it was shown, into one steady clip; a [Revideo](https://github.com/midrender/revideo) (MIT)
project in `web/e2e/marketing/compose/` (installed once, with the repo's pinned pnpm, in the Playwright
image) then renders the pans and zooms over it, offline, with its telemetry off, before the budget encode. While the camera is on a box, the rest of the capture dims to
half, so the eye lands on what changed, and the box (with a 12-pixel margin) stays lit; a click shows a
teal ripple at the pointer. `frameFor`, `cameraTrack`, `spotFor` and `concatList` in `marketinglib.mjs`
are the framing, the move list, the lit box and the frame timing, unit tested.

A `wait` may take `lapse: N` (seconds, up to 5): the quiet seconds before something changes play in N
seconds of the clip, a time-lapse, sped up as much as that run's wait needs, up to 40 times
(`lapseSpeed`), and never slowed down. A loop has 9 seconds to spend and a console that needs five to
recover (an outpost's next check-in after a credential is fixed) is not one to sit through. The lapse
fixes the wait's length in the clip, not its speed, so a slower lab makes a faster lapse rather than a
longer loop; a wait past 40 times its lapse plays longer, and the budget catches it. The clip says
nothing false: a sped-up stretch that lasts at least 0.6 seconds on screen carries a badge reading "N×
speed" with that run's speed (`lapseBadges`), so an alarm that takes a minute to raise arrives on camera
in a second and is never taken for real time.

Setup runs off camera, in order, as the lab's owner. `put`, `post` and `patch` call the API (a `{braced}`
part of a path is one address, percent-encoded whole, so a component's path address names one route
segment), and `wait` polls a GET until a dotted field equals a value. This is how a fault is staged on the
fleet: the main display of `hq-mr86-1` unplugged through the impairment route, a video bar's login
repointed at a secret the device refuses. The shipped reachability rule needs about a minute to hold its
alarm. A loop that shows the fault arriving (`alarm-outage`) unplugs the display as its last setup item and
waits for the alarm on camera in a `lapse: 0.9`; the others record the console reading a fault that is already
true. The shots run in the order listed on one lab, and a later shot relies on the state an earlier setup
left.

A `call` step is the same request in the middle of a clip, off camera: `call: { put: <path>, body: {...} }`.
It is what happens in the room while the console shows what it makes of it: the teaser
(`omniglass-teaser`) plugs the boardroom's display back in with one, then waits in a `lapse: 0.6` for the room
to read healthy. It takes no focus, since nothing on screen moves for it, and the preflight checks the
resource it names as it does a setup item's.

One thing is hidden and nothing else: the lab banner. The capture-only stylesheet (`CAPTURE_CSS` in
[`web/e2e/marketinglib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/marketinglib.mjs))
removes placeholders, never data: no rule reaches a table, a row, a figure or a state.

`MARKETING_DEST` must be set, absolute and an existing directory, and the shot list must parse, with an
omnibot recording the lab ships; all of that is checked before the lab starts. Once the lab is up and
before the first shot plays, every component, system and endpoint the list names is checked on that
lab, so a typo or a fleet that changed fails in seconds, naming the shot. `make test-marketing-shots`
proves each of those failures, and the shot list's parsing, budget and manifest are unit tested in
`web/src/marketing-shots.test.ts`.

## Recording details

Each recorder runs on a lab of its own. `make demo ID=<id>` records one demo of either kind
(`ID="<a> <b>"` several, no `ID` every demo), each recorder on a lab of its own (it runs `make` first,
since the lab is the binary and the image `make` builds); when a PR needs
which, and how to write and review each, are in [demos](/contributing/demos/).

**A terminal demo** is a [VHS](https://github.com/charmbracelet/vhs) tape, `scripts/demo/<id>.tape`.
VHS runs from its own image, `ghcr.io/charmbracelet/vhs` at v0.12.1, pinned by digest in
[`web/e2e/tape/Dockerfile`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tape/Dockerfile);
the image bundles what VHS drives (ttyd for the terminal, chromium to render it, ffmpeg to encode the
GIF and MP4) and the fonts the house settings name, and publishes a multi-platform index, so it runs
on amd64 and arm64 hosts alike. The recorder adds `jq`, the prelude every tape sources, and this
tree's `omniglass` binary, copied out of the lab's image, so a tape types the CLI the lab serves.
Nothing is installed on the host. `make demo` writes `scripts/demo/out/<id>/`: the GIF, the MP4 and
`demo.json`, the manifest a passing recording carries. A terminal demo is a test: a command it types
that exits non-zero, a wait in it that runs out, or a recording over its budget (`MAX_GIF_BYTES` and
`MAX_SECONDS` in
[`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs))
fails the run, naming the tape and the cause. `make test-demos` proves each of those failures.

**A walkthrough** is a script, `web/e2e/walkthroughs/<id>.yaml` (the id is lowercase letters, digits and
hyphens): a `title`, an optional `description`, and `steps`, each one kind of action or a wait for a
visible result, with a `caption` shown while it runs. The step kinds, their fields and the rules are
defined, with an annotated example, at the top of
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs).
The format is validated strictly: an unknown key or kind fails, naming the file and the step, and so does a
script that ends on an action rather than a wait for its result. An optional `setup` makes, through the
API and off camera, what the scene needs that the walkthrough does not teach (set-up-monitoring's
display in Bay 2, which nothing collects from yet); one that already exists counts as made.

```yaml
title: Read a simulated component's status
steps:
  - goto: /simulation
    caption: The Simulation page shows the simulated fleet, live.
  - click: '[data-testid="sim-row"] >> nth=0'
    caption: Open a simulated component.
  - wait: { selector: '[data-testid="sim-status"]' }
    caption: Its status says where it is served, and how often collection has reached it.
    poster: true
```

The recording's ffmpeg image is pinned by digest to an amd64-only image (its tag publishes no
multi-platform index), so recording on an arm64 host needs amd64 emulation registered with Docker.

`make walkthroughs` records every script on a lab (`make walkthroughs ID=<id>` records
one) into `web/e2e/walkthroughs/out/<id>/`: an mp4 opening on a title card, with a cursor that glides to
each target and ripples where it clicks, a highlight on the target and a caption bar; `preview.webp`, an
animated preview encoded from the mp4 by the same pinned ffmpeg (a lower frame rate and width, looping),
which is what the PR comment and a docs page play inline; `poster.png`; and a still per step. A walkthrough
is a test: a step whose result never appears fails the run, naming the step, its kind and what it waited
for, and keeps the stills taken so far; so does a recording over the budget, whose limits on length, mp4
size and preview size are `MAX_SECONDS`, `MAX_BYTES` and `MAX_PREVIEW_BYTES` in
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs). `make test-walkthroughs` proves both failures, an over-budget preview among them.
When a PR needs a walkthrough, how to review its stills, and how a docs page embeds one are in
[demos](/contributing/demos/).
