---
title: "Labels"
description: "The fixed label taxonomy: what is a native Issue Type, what is a Project field, and what stays a label."
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
  (`High`/`Medium`/`Low`), never a `prio:*` label. It sorts and groups on the board, where
  planning actually happens.

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

`run:test` and `run:build` are retired, not reserved: the full test suite is the merge gate, so it
runs on every PR rather than on demand (ruled on #214: a test outside the CI gate is a gate
defect), and the release build runs on every PR that touches its config, so neither has a task to
run, and a leftover label of either name does nothing. A `run:<task>` label is added in the same
PR that wires its workflow, never reserved ahead of it, since a label that silently does nothing
breaks the promise above.

## The fixed set

That is the whole taxonomy: a native Issue Type, the Project priority field, `area:*`, and
`run:*`. There are no stock GitHub labels (`bug`, `enhancement`, `good first issue` and the
rest were removed) and no bare `type:*` or `prio:*` labels. Adding a label means either a
new subsystem (`area:*`) or a new automation task (`run:*`); anything else belongs in a
field.
