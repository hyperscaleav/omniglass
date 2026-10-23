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
| `run:walkthrough` | records the PR's walkthroughs in CI and upserts a comment that links a scrubbable video of each and plays its animated preview, with its poster and stills below | yes |
| `run:build` | rebuilds the release binaries and container image for the PR head on demand | reserved |
| `run:test` | runs the full test suite on demand | reserved |

`run:preview` is live today (the [PR previews guide](/guides/pr-previews/) covers it, and
`preview-comment.yml` is the workflow behind it). So is `run:walkthrough`
(`walkthrough.yml`). Applying it records walkthroughs on the walkthrough stack: for a PR that
changes nothing under `web/` but walkthrough scripts, just the scripts it adds or changes;
otherwise every walkthrough, since any console change can move a flow. It then commits each
passing walkthrough's mp4, animated preview (`preview.webp`), poster and per-step stills under
`<id>/` to the PR's own evidence ref, `refs/walkthroughs/pr-<n>`, as a fresh tree (so a renamed or
deleted script leaves nothing behind) on top of the ref's last commit, and keeps one comment on the
PR reaching them by that commit's SHA: each walkthrough leads with a **Watch (scrubbable)** link to
its mp4 through jsDelivr, which serves it as video with range requests so the browser's player
seeks, then its preview, which plays on the PR page, then its poster and its captioned stills folded
below. The comment also links the run's workflow artifact (which keeps a failed walkthrough's
partial stills) and names any walkthrough that failed (a walkthrough is a test, so a failure fails
the job; a stack that never came up is reported as that, not as every walkthrough failing). Why a
ref rather than a branch is in [walkthroughs](/contributing/walkthroughs/#show-it-on-the-pr).

- **What it costs:** nothing on main. The evidence never touches the PR branch, so a squash-merge
  carries none of it, and a default clone does not fetch the ref. It does sit in the repository's
  object store on GitHub (about 2 to 3.5 MB per walkthrough per run, video included).
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
