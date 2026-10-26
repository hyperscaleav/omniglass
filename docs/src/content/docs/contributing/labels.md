---
title: "Labels and the board"
description: "The fixed label taxonomy (what is a native Issue Type, what is a Project field, what stays a label) and the project board that tracks the work: its fields, who moves what, its views."
---

Labels are a small, fixed set. Two whole categories that would otherwise be labels
are not labels here: they live in first-class GitHub fields instead, and labels are
reserved for the axes those fields do not cover.

## What is not a label

- **Kind of work** is a native **Issue Type** (`Task`, `Bug`, `Feature`, `Epic`), set on
  the issue itself, never a `type:*` label. It maps to the conventional-commit type the
  eventual PR carries: `Task` = ci/docs/chore/refactor/test/perf, `Bug` = fix,
  `Feature` = feat, `Epic` groups a body of work. One field, filterable, with no parallel
  label to drift out of sync.
- **Priority** is a single-select **field on the Omniglass Project board**
  (`High`/`Medium`/`Low`), never a `prio:*` label. It sorts and groups on
  [the board](#the-board), where planning actually happens, and the architect alone sets it.

Using a label for either would duplicate a field GitHub already models, and two systems
for one fact drift apart.

## What is a label

Everything left is one of two prefixed families. The prefix encodes the one distinction
that matters: does the label **describe** the issue, or does it **trigger** automation?

### `area:<subsystem>` describes

The subsystem a change touches, named to match the architecture glossary. The set grows as
subsystems land; today:

`area:foundation` `area:api` `area:storage` `area:auth` `area:collection` `area:expr`
`area:ui` `area:node` `area:health` `area:settings` `area:tags` `area:files`

Purely descriptive: it filters the issue list by subsystem. An issue can carry more than
one.

### `run:<task>` triggers

A label that makes CI *do something* when it is applied. The `run:` prefix is a two-way
promise: if a label runs automation it is named `run:<task>`, and if a label is named
`run:<task>` then applying it runs that task.

| Label | Applying it | Wired |
| --- | --- | --- |
| `run:preview` | spins up a live [PR preview environment](/guides/pr-previews/); removing it tears the preview down | yes |
| `run:demo` | records the [demos](/contributing/demos/) the PR body tags (`demo: <id>`) in CI, upserts one comment listing every demo, and writes each demo's clip into the PR body's Demo section | yes |
| `run:build` | rebuilds the release binaries and container image for the PR head on demand | reserved |
| `run:test` | runs the full test suite on demand | reserved |

`run:preview` is live today (the [PR previews guide](/guides/pr-previews/) covers it, and
`preview-comment.yml` is the workflow behind it). So is `run:demo` (`demo.yml`). Applying it
records the demos the PR is about, each on a runner and a walkthrough stack of its own, in
parallel: exactly the demos its body tags in its Demo section (`demo: <id>`, a tape or a
walkthrough), plus any tape (`scripts/demo/<id>.tape`) or walkthrough script it adds or changes.
Nothing else fans out: touching the console, a recorder or the stack records no demo the PR did
not name (the nightly freshness run records every demo instead). A PR with no tag fails the run,
saying how to tag; `demo: none (<reason>)` records nothing. It then commits
each passing demo's files under `<id>/` to the PR's own evidence ref, `refs/walkthroughs/pr-<n>`,
as a fresh tree (so a renamed or deleted demo leaves nothing behind) on top of the ref's last
commit, and keeps **one comment** on the PR listing every demo and reaching its files by that
commit's SHA: each demo leads with a **Watch (scrubbable)** link to its video through jsDelivr,
which serves it as video with range requests so the browser's player seeks, then what plays on the
PR page (a terminal demo's GIF; a walkthrough's animated preview, then its poster and its captioned
stills folded below). The comment also links the run's workflow artifacts (which keep a failed
demo's partial output) and names any demo that failed (a demo is a test, so a failure fails its
job; a demo whose stack never came up is reported as that, not as a failure). The run also writes
each demo's clip (its GIF or animated preview and its scrubbable link) into the PR body's Demo
section, replacing only its own marked block. Why a ref rather than a branch is in
[demos](/contributing/demos/#show-it-on-the-pr).

`run:walkthrough` is `run:demo`'s earlier name, from when walkthroughs were the only demos. The
workflow still honors it, so a PR that carries it (or a re-run of a run it started) records as
before, and a PR's existing walkthrough comment is found and updated in place rather than joined by
a second one. It is not a second task: apply `run:demo`. The run plays the branch's own recorders,
so a branch cut before demos merges main first; the job says so if it has not. Retiring the old
name is [#1136](https://github.com/hyperscaleav/omniglass/issues/1136).

- **What it costs:** nothing on main. The evidence never touches the PR branch, so a squash-merge
  carries none of it, and a default clone does not fetch the ref. It does sit in the repository's
  object store on GitHub and in any mirror of every ref (a few MB per demo per run, video
  included), until `demo-prune.yml` deletes the ref 30 days after the PR closes.
- **The PR's checks:** untouched. The run pushes only the evidence ref, never the PR head, so there
  is nothing to pull after a run and no check left un-run on the head.

- **What it records:** the branch tip when the job runs, not the commit the label was applied
  at. The label stays on; re-run the job, or remove and re-add the label, to record the current
  head again, and the comment updates in place.
- **Merge conflicts:** GitHub runs no `pull_request` workflow on a PR with merge conflicts, so
  the label does nothing until they are resolved.
- **Forks:** fork PRs are skipped (their token cannot push).

`run:build` and `run:test` are reserved:
the expensive build and test tiers run on every PR today, and
[issue #78](https://github.com/hyperscaleav/omniglass/issues/78) tracks moving them behind
these labels so they run only on demand. Wire the workflow in the same PR that first relies
on a reserved label, rather than leaving a label that silently does nothing.

## The fixed set

That is the whole taxonomy: a native Issue Type, the Project priority field, `area:*`, and
`run:*`. There are no stock GitHub labels (`bug`, `enhancement`, `good first issue` and the
rest were removed) and no bare `type:*` or `prio:*` labels. Adding a label means either a
new subsystem (`area:*`) or a new automation task (`run:*`); anything else belongs in a
field.

## The board

The [Omniglass project](https://github.com/orgs/hyperscaleav/projects/1) (org project #1) is
the live status of the work. An issue holds the work itself; the board holds where it
stands: filed, planned, being built, waiting on review, done. Before it was moved by a
script, it held 27 stale items against 249 open issues, and status lived only in chat and
in whether an issue was open. Now it moves at fixed points in the lifecycle, by one
script the skills call and by GitHub's own workflows, so reading the board is reading the
state of the work.

### The fields

- **Status** is where the work stands: `Backlog` (filed, not yet planned), `Todo` (planned
  next), `In Progress` (being built), `In Review` (its PR is open), `Done` (closed). Every
  item has one.
- **Priority** is `High`, `Medium`, `Low`, or blank. Blank means nobody has weighed it yet,
  which is what the Needs you view collects.
- **Iteration** is a two-week window of planned work, defined in the field's settings.

### Who moves what

Agents move **Status**, and only at fixed points, each a call written into the skill that
owns the moment:

| The moment | The skill | The call |
| --- | --- | --- |
| a definition is posted | `/define-work` | `scripts/board.sh add <n>`: Backlog |
| a finding is filed | `/file-bug` | `scripts/board.sh add <n>`: Backlog |
| a loop starts, and each slice in it | `/run-feature-loop` | `scripts/board.sh set <n> "In Progress"` |
| the PR opens | `/ship-slice` | `scripts/board.sh set <n> "In Review"` |
| the day's slice starts, then its PR opens | `/autopilot` | the same two moves, listed in the day report |
| the issue closes (its PR merged) | GitHub | `Done`, by the built-in workflow |

The architect alone sets **Priority** and picks the **Iteration**. No skill writes either,
and the script cannot write Priority at all: a Priority name given as a status is refused,
and its tests fail on any call that touches the field. The architect may move any Status by
hand as well, most often Backlog to Todo when planning.

### The script

`scripts/board.sh` is the one way an agent moves the board, so nobody hand-types a field or
option id:

```bash
scripts/board.sh add 1191                # on the board in Backlog; an item already there keeps its Status
scripts/board.sh set 1191 "In Progress"  # Backlog | Todo | In Progress | In Review | Done
scripts/board.sh iteration 1191 current  # the iteration running today (the architect's)
scripts/board.sh show 1191               # its Status, Priority and Iteration
```

- **By name.** It resolves the project, its fields, their options and the issue's item by
  name each time it runs, once per run, so a renamed option fails loudly rather than
  writing the wrong one, and adding an option takes no change to the script.
- **Idempotent.** A second run reads what the first wrote and changes nothing
  (`#1191 is already In Progress: unchanged`). `set` and `iteration` put an issue that is not
  on the board there first, and `add`, `set` and `iteration` restore an archived item.
- **In Review before it exists.** Until the board has an `In Review` status, a move to it
  lands on `In Progress` with one warning.
- **Errors a caller can act on.** A token without the `project` scope, an issue the repository
  does not have (a pull request number is not an issue), a project it cannot see, and a
  status the board lacks each say so and what to do. It needs `jq` and `gh` authenticated
  with the `project` scope (`gh auth refresh -s project`), which is why a cloud sandbox with
  only the GitHub MCP tools cannot run it: a skill there names the move it could not make.

Its tests run with `go test ./scripts/` (and so in `make test-short`) against a fake `gh`
that answers from the board's field list as recorded, `scripts/testdata/board/field-list.json`,
and refuses any id the project does not have. When the board's fields or options change,
record it again:
`gh project field-list 1 --owner hyperscaleav --format json --limit 100 | jq . > scripts/testdata/board/field-list.json`.

### The views

Four saved views, each a tab on the project:

- **Roadmap** (roadmap layout, dates from Iteration, filter `is:open`, sorted by Priority):
  the iterations as a timeline and what is planned into each. The architect plans here,
  dragging work between iterations.
- **Board** (board layout, columns from Status, filter `-status:Backlog`): the work in
  motion, one column per state, from Todo to Done.
- **Needs you** (table layout, filter `is:open no:priority`, grouped by Status): open work
  nobody has prioritized, the architect's triage queue. It is empty when triage is done.
- **In flight** (table layout, filter `status:"In Progress","In Review"`, grouped by Status,
  sorted by Updated, newest first): what agents are building, and in its In Review group,
  the PRs waiting on the architect's review.

### One-time setup in the UI

GitHub's API cannot configure a project's built-in workflows or create its views, so these
are set once by hand in the project's settings and its Workflows page:

1. **Add the In Review status.** In the Status field's settings, add the option
   `In Review` between `In Progress` and `Done`. The script finds it by name from then on,
   and the fallback warning stops.
2. **Item closed sets Done.** Workflows, "Item closed": set Status to `Done`, and enable it.
3. **Pull request merged sets Done.** Workflows, "Pull request merged": set Status to `Done`,
   and enable it. Issues reach Done by closing; this covers a pull request put on the board
   by hand.
4. **Auto-add from this repository.** Workflows, "Auto-add to project": the repository
   `hyperscaleav/omniglass`, filter `is:issue is:open`, enabled. Then "Item added to project":
   set Status to `Backlog`, so an issue auto-added lands where `scripts/board.sh add` would
   put it. Auto-add catches issues created or updated after it is enabled; an older issue
   goes on with `scripts/board.sh add`.
5. **The four views** above: a new view per tab, its layout, filter, grouping and sort, saved.
6. **Iterations.** In the Iteration field's settings, keep a current iteration defined:
   `scripts/board.sh iteration <n> current` reports that none is running until one is.
